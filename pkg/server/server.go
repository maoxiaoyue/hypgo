// Package server 提供 HTTP/1.1/2/3 統一伺服器實現
//
// @chris
package server

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// strongCipherSuites 統一的安全 cipher suite 列表，所有協議共用
var strongCipherSuites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
}

// Server 統一的 HTTP 伺服器
type Server struct {
	config *config.Config
	router *router.Router
	// httpServer/h3Server 用 atomic.Pointer：Start() 的寫入與 Health()/Shutdown()
	// 的讀取可能發生在不同 goroutine（例如 Start() 尚未完成初始化時 Health
	// 端點已被打），純指標欄位會是資料競態
	httpServer atomic.Pointer[http.Server]
	h3Server   atomic.Pointer[http3.Server]
	logger     *logger.Logger
	// listener 同理用 atomic.Value（net.Listener 是介面型別，atomic.Value 存取
	// 介面值比 atomic.Pointer[net.Listener] 少一層指標間接）：forkNewProcess
	// 讀取時若剛好與 startHTTPx 的寫入同時發生（例如極早的 graceful restart
	// 訊號），裸欄位會是資料競態
	listener atomic.Value

	// 協議檢測
	protocol Protocol

	// 0-RTT 支援（帶 LRU 淘汰 + TTL）
	sessionCache *SessionCache

	// Alt-Svc 標頭值（啟動時預計算；空字串 = 不廣告 H3）
	altSvcValue string

	// 優雅關閉（atomic 避免競態；Once 保證 Shutdown 冪等）
	shutdownChan chan struct{}
	shuttingDown atomic.Bool
	shutdownOnce sync.Once
}

// getListenerAtomic 取得目前已發布的 listener（可能為 nil，尚未啟動時）
func (s *Server) getListenerAtomic() net.Listener {
	v := s.listener.Load()
	if v == nil {
		return nil
	}
	return v.(net.Listener)
}

// Protocol 協議類型
type Protocol int

const (
	HTTP1 Protocol = iota
	HTTP2
	HTTP3
	AUTO // 自動檢測
)

// sessionEntry 帶時間戳的 session 快取項（鏈表節點值）
type sessionEntry struct {
	key       string
	data      []byte
	createdAt time.Time
}

// SessionCache 0-RTT session 快取（帶大小上限 + TTL）
//
// entries 提供 O(1) 查找；order 依插入時間排序（頭部最舊），
// 過期清理與容量淘汰都從頭部彈出——O(1)（攤銷）。
// 舊實作淘汰時對整個 map（上限 1 萬條目）做 O(n) 全表掃描，
// 且掃描發生在每次新握手的鎖內：重連風暴時所有 TLS 握手
// 在這個臨界區後面全域序列化
type SessionCache struct {
	entries map[string]*list.Element
	order   *list.List // 值為 *sessionEntry；front = 最舊
	mu      sync.Mutex
	maxSize int
	ttl     time.Duration
}

const (
	defaultSessionCacheSize = 10000
	defaultSessionTTL       = 24 * time.Hour
)

// newSessionCache 建立帶預設值的 SessionCache
func newSessionCache() *SessionCache {
	return newSessionCacheWith(defaultSessionCacheSize, defaultSessionTTL)
}

// newSessionCacheWith 建立指定容量與 TTL 的 SessionCache（測試用）
func newSessionCacheWith(maxSize int, ttl time.Duration) *SessionCache {
	return &SessionCache{
		entries: make(map[string]*list.Element, 256),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Put 儲存 session，超過上限時淘汰最舊的條目
func (c *SessionCache) Put(key string, state []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 淘汰過期條目（從最舊端彈出，攤銷 O(1)）
	c.evictExpiredLocked()

	// 超過上限時淘汰最舊（O(1)：鏈表頭即最舊）
	if len(c.entries) >= c.maxSize {
		c.removeElementLocked(c.order.Front())
	}

	// 同 key 重複 Put：先移除舊條目，避免鏈表殘留孤兒節點
	if old, ok := c.entries[key]; ok {
		c.removeElementLocked(old)
	}

	elem := c.order.PushBack(&sessionEntry{
		key:       key,
		data:      state,
		createdAt: time.Now(),
	})
	c.entries[key] = elem
}

// GetAndDelete 原子性地取得並刪除 session（防止 0-RTT replay attack）
// 回傳 false 表示不存在或已過期
func (c *SessionCache) GetAndDelete(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*sessionEntry)

	// 原子 get-and-delete，防止 race window 重放（過期條目也一併移除）
	c.removeElementLocked(elem)

	if time.Since(entry.createdAt) > c.ttl {
		return nil, false
	}
	return entry.data, true
}

// evictExpiredLocked 淘汰過期條目（必須持有鎖）
// 條目依插入時間排序，從最舊端彈出直到遇到未過期者即可停止
func (c *SessionCache) evictExpiredLocked() {
	now := time.Now()
	for {
		front := c.order.Front()
		if front == nil {
			return
		}
		if now.Sub(front.Value.(*sessionEntry).createdAt) <= c.ttl {
			return
		}
		c.removeElementLocked(front)
	}
}

// removeElementLocked 同步移除鏈表節點與 map 條目（必須持有鎖）
func (c *SessionCache) removeElementLocked(elem *list.Element) {
	if elem == nil {
		return
	}
	entry := elem.Value.(*sessionEntry)
	c.order.Remove(elem)
	delete(c.entries, entry.key)
}

// Len 回傳目前快取條目數量
func (c *SessionCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// New 創建新的伺服器實例
func New(cfg *config.Config, log *logger.Logger) *Server {
	return &Server{
		config:       cfg,
		router:       router.New(),
		logger:       log,
		sessionCache: newSessionCache(),
		shutdownChan: make(chan struct{}),
	}
}

// Router 返回路由器
func (s *Server) Router() *router.Router {
	return s.router
}

// Use 添加全域中間件
func (s *Server) Use(middlewares ...hypcontext.HandlerFunc) {
	s.router.Use(middlewares...)
}

// Start 根據配置啟動伺服器
func (s *Server) Start() error {
	// 保存 PID 檔案
	if err := s.savePIDFile(); err != nil {
		s.logger.Warningf("Failed to save PID file: %v", err)
	}

	// 將 BindInput 型別不符回報接到 logger（context 對 logger 零依賴，故以 hook 注入）
	hypcontext.SetBindInputReporter(func(routeKey, declared, bound string) {
		s.logger.Warningf("BindInput 型別不符 [%s]：handler 綁定 %s，但 Schema 宣告 %s", routeKey, bound, declared)
	})

	// 未消費的請求錯誤（c.Error / AbortWithError）接到 logger，
	// 否則這些錯誤只會送出空的 5xx、內容徹底消失
	hypcontext.SetRequestErrorReporter(func(method, path string, errs []error) {
		for _, err := range errs {
			s.logger.Error("request error", "method", method, "path", path, "err", err)
		}
	})

	// 可信代理：未設定時 ClientIP 一律回傳實際連線來源（安全預設）
	if err := hypcontext.SetTrustedProxies(s.config.Server.TrustedProxies...); err != nil {
		s.logger.Warningf("Invalid trusted_proxies config: %v", err)
	}

	// AutoSync：啟動時自動同步 .hyp/context.yaml
	sync := manifest.NewAutoSync(
		manifest.AutoSyncConfig{Enabled: true},
		s.router, s.config, s.logger,
	)
	sync.SyncSafe()

	// 套用預設中間件（Recovery / Logger / Security / CORS；http3 時為 HTTP3Middleware）。
	// v0.8.11 前僅 ListenAndServeWithGracefulShutdown 會套用（而它無人呼叫），
	// 主要入口（App.Run / srv.Start）一直跑在無 Recovery、無請求日誌、無安全頭的狀態
	s.applyDefaultMiddlewares()

	// 監聽 SIGINT / SIGTERM，收到即優雅關閉。
	// v0.8.11 前主要入口完全沒有訊號處理：Ctrl+C / k8s SIGTERM = 行程直接被殺，
	// in-flight 請求丟失、PID 檔殘留
	go s.handleShutdownSignals()

	// 設置優雅重啟處理
	if s.isGracefulRestartEnabled() {
		go s.handleGracefulRestart()
	}

	// Alt-Svc 預計算：值在啟動後不變，先前每請求 fmt.Sprintf（格式
	// 解析 + 2 次分配）。只在 auto 模式（真的同時起 H3 listener）
	// 廣告——舊行為只看 TLS，純 http1/http2 模式也發 Alt-Svc，
	// 誘使客戶端做註定失敗的 QUIC 連線嘗試
	if s.config.Server.TLS.Enabled && s.config.Server.Protocol == "auto" {
		s.altSvcValue = fmt.Sprintf(`h3="%s"; ma=86400`, s.config.Server.Addr)
	}

	// 自動檢測協議或使用指定協議
	var err error
	if s.config.Server.Protocol == "auto" {
		err = s.startAutoProtocol()
	} else {
		switch s.config.Server.Protocol {
		case "http3", "h3":
			err = s.startHTTP3()
		case "http2", "h2":
			err = s.startHTTP2()
		default:
			err = s.startHTTP1()
		}
	}

	// 優雅關閉時 Serve/ListenAndServe 依 stdlib 慣例回傳 http.ErrServerClosed
	// （quic-go http3 v0.59+ 亦同）——屬預期流程而非錯誤，過濾掉，
	// 避免呼叫端（main.go / App.Run）在正常關閉時誤報 Server error。
	// net.ErrClosed 同理：關閉流程中 listener 先一步關掉時 Accept 的收場錯誤
	if errors.Is(err, http.ErrServerClosed) || (errors.Is(err, net.ErrClosed) && s.shuttingDown.Load()) {
		return nil
	}
	return err
}

// startAutoProtocol 自動協議選擇（同時支援 HTTP/1.1/2/3）
func (s *Server) startAutoProtocol() error {
	s.logger.Infof("Starting server with auto protocol detection on %s", s.config.Server.Addr)

	// 啟動 HTTP/3 伺服器（UDP）
	if s.config.Server.TLS.Enabled {
		go func() {
			if err := s.startHTTP3(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.logger.Warningf("HTTP/3 server failed: %v", err)
			}
		}()
	}

	// 啟動 HTTP/1.1 + HTTP/2 伺服器（TCP）
	return s.startHTTP2WithFallback()
}

// getTLSWrapSession 取得用於 TLS 1.3 0-RTT 的 WrapSession 函數
func (s *Server) getTLSWrapSession() func(tls.ConnectionState, *tls.SessionState) ([]byte, error) {
	return func(cs tls.ConnectionState, ss *tls.SessionState) ([]byte, error) {
		ticket := make([]byte, 32)
		if _, err := rand.Read(ticket); err != nil {
			return nil, err
		}
		stateBytes, err := ss.Bytes()
		if err != nil {
			return nil, err
		}
		// 直接以原始位元組為 map key：Go map key 可為任意二進位字串，
		// hex 編碼徒增每握手一次 64B 字串分配與雙倍 key 記憶體
		s.sessionCache.Put(string(ticket), stateBytes)
		return ticket, nil
	}
}

// getTLSUnwrapSession 取得用於 TLS 1.3 0-RTT 的 UnwrapSession 函數
// 使用 GetAndDelete 原子操作防止 replay attack
func (s *Server) getTLSUnwrapSession() func([]byte, tls.ConnectionState) (*tls.SessionState, error) {
	return func(identity []byte, cs tls.ConnectionState) (*tls.SessionState, error) {
		stateBytes, ok := s.sessionCache.GetAndDelete(string(identity))
		if !ok {
			return nil, nil
		}
		return tls.ParseSessionState(stateBytes)
	}
}

// loadCertificate 載入 TLS 證書（回傳 error 而非 panic）
func (s *Server) loadCertificate() (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(
		s.config.Server.TLS.CertFile,
		s.config.Server.TLS.KeyFile,
	)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load TLS certificate: %w", err)
	}
	return cert, nil
}

// startHTTP3 啟動 HTTP/3 伺服器
func (s *Server) startHTTP3() error {
	s.logger.Infof("Starting HTTP/3 server on %s", s.config.Server.Addr)

	if !s.config.Server.TLS.Enabled {
		return fmt.Errorf("HTTP/3 requires TLS to be enabled")
	}

	cert, err := s.loadCertificate()
	if err != nil {
		return err
	}

	// 配置 TLS（HTTP/3 要求 TLS 1.3）
	tlsConfig := &tls.Config{
		Certificates:  []tls.Certificate{cert},
		NextProtos:    []string{"h3"},
		MinVersion:    tls.VersionTLS13,
		WrapSession:   s.getTLSWrapSession(),
		UnwrapSession: s.getTLSUnwrapSession(),
	}

	// QUIC 配置：把 config 的調校值接進 HTTP/3（與 TCP 側一致），
	// 並啟用 0-RTT——SessionCache 的 WrapSession/UnwrapSession 機制
	// 只有在 Allow0RTT=true（quic-go 預設 false）時才真正省下握手 RTT；
	// GetAndDelete 的單次使用語義已防範 0-RTT 重放
	maxStreams := s.config.Server.MaxConcurrentStreams
	if maxStreams <= 0 {
		maxStreams = 250
	}
	quicConf := &quic.Config{
		Allow0RTT:          true,
		MaxIdleTimeout:     time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxIncomingStreams: int64(maxStreams),
	}

	// 創建 HTTP/3 伺服器
	h3srv := &http3.Server{
		Handler:         s.wrapH3Handler(),
		Addr:            s.config.Server.Addr,
		TLSConfig:       tlsConfig,
		QUICConfig:      quicConf,
		EnableDatagrams: false,
		MaxHeaderBytes:  1 << 20,
	}
	s.h3Server.Store(h3srv)

	// 監聽並服務
	return h3srv.ListenAndServe()
}

// startHTTP2WithFallback 啟動 HTTP/2 伺服器（支援 HTTP/1.1 降級）
func (s *Server) startHTTP2WithFallback() error {
	s.logger.Infof("Starting HTTP/2 server with HTTP/1.1 fallback on %s", s.config.Server.Addr)

	// 驗證並修正 HTTP/2 設定
	maxReadFrameSize := s.config.Server.MaxReadFrameSize
	if maxReadFrameSize < 16384 {
		maxReadFrameSize = 16384 // HTTP/2 spec 最小值
	}
	if maxReadFrameSize > 16777215 {
		maxReadFrameSize = 16777215 // HTTP/2 spec 最大值
	}

	maxConcurrentStreams := s.config.Server.MaxConcurrentStreams
	if maxConcurrentStreams <= 0 {
		maxConcurrentStreams = 250
	}

	// 配置 HTTP/2
	h2s := &http2.Server{
		MaxHandlers:                  s.config.Server.MaxHandlers,
		MaxConcurrentStreams:         uint32(maxConcurrentStreams),
		MaxReadFrameSize:             uint32(maxReadFrameSize),
		PermitProhibitedCipherSuites: false,
		IdleTimeout:                  time.Duration(s.config.Server.IdleTimeout) * time.Second,
	}

	// 獲取或創建監聽器
	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener.Store(listener)

	// 明文模式才需要 h2c（cleartext HTTP/2 升級）；TLS 之上 h2c 升級
	// 不可能發生，卻讓每個請求白付 PRI/Upgrade header 探測成本
	var handler http.Handler = s.router
	if !s.config.Server.TLS.Enabled {
		handler = h2c.NewHandler(s.router, h2s)
	}
	handler = s.wrapHandler(handler)

	// 創建 HTTP 伺服器
	httpSrv := &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// TLS 配置（統一 cipher suites）
	if s.config.Server.TLS.Enabled {
		httpSrv.TLSConfig = &tls.Config{
			NextProtos:    []string{"h2", "http/1.1"},
			MinVersion:    tls.VersionTLS12,
			CipherSuites:  strongCipherSuites,
			WrapSession:   s.getTLSWrapSession(),
			UnwrapSession: s.getTLSUnwrapSession(),
		}
		// 把調校過的 h2s 掛上 TLS 協商出的 h2：不呼叫 ConfigureServer 的話，
		// net/http 會用自帶的 http2 實作與「預設值」服務 h2 流量——
		// config 的 MaxConcurrentStreams / MaxReadFrameSize 等調校
		// 對生產 HTTPS 完全不生效（只有 h2c 明文路徑吃得到）
		if err := http2.ConfigureServer(httpSrv, h2s); err != nil {
			return fmt.Errorf("http2.ConfigureServer: %w", err)
		}
		s.httpServer.Store(httpSrv)
		return httpSrv.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	s.httpServer.Store(httpSrv)
	return httpSrv.Serve(listener)
}

// startHTTP2 啟動純 HTTP/2 伺服器
func (s *Server) startHTTP2() error {
	// 強制 HTTP/2
	s.protocol = HTTP2
	return s.startHTTP2WithFallback()
}

// startHTTP1 啟動 HTTP/1.1 伺服器
func (s *Server) startHTTP1() error {
	s.logger.Infof("Starting HTTP/1.1 server on %s", s.config.Server.Addr)
	s.protocol = HTTP1

	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener.Store(listener)

	handler := s.wrapHandler(s.router)

	httpSrv := &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// TLS 配置（統一 cipher suites）
	if s.config.Server.TLS.Enabled {
		httpSrv.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			CipherSuites: strongCipherSuites,
		}
		s.httpServer.Store(httpSrv)
		return httpSrv.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	s.httpServer.Store(httpSrv)
	return httpSrv.Serve(listener)
}

// wrapHandler 包裝處理器以注入 Alt-Svc 標頭（值於 Start 預計算；
// 無 H3 可廣告時不加包裝層）
func (s *Server) wrapHandler(h http.Handler) http.Handler {
	altSvc := s.altSvcValue
	if altSvc == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor < 3 {
			w.Header().Set("Alt-Svc", altSvc)
		}
		h.ServeHTTP(w, r)
	})
}

// wrapH3Handler 包裝 HTTP/3 處理器
func (s *Server) wrapH3Handler() http.Handler {
	// 直接回傳 router：先前多包一層只轉呼叫的 HandlerFunc，
	// 每請求平白多一個 call frame
	return s.router
}

// detectProtocol 檢測請求使用的協議
func (s *Server) detectProtocol(r *http.Request) string {
	switch r.ProtoMajor {
	case 3:
		return "HTTP/3"
	case 2:
		return "HTTP/2"
	default:
		return "HTTP/1.1"
	}
}

// getListener 創建或繼承監聽器
func (s *Server) getListener() (net.Listener, error) {
	if ln := s.getInheritedListener(); ln != nil {
		return ln, nil
	}
	return net.Listen("tcp", s.config.Server.Addr)
}

// getInheritedListener 獲取繼承的監聽器（帶驗證）
func (s *Server) getInheritedListener() net.Listener {
	// FD 3 必須是「正在 listening」的 socket 才視為 parent 傳入的 listener。
	// net.FileListener 對已連線的 TCP socket 也會成功回傳 *net.TCPListener，
	// 少了這層 kernel 檢查，shell 湊巧留在 FD 3 的 stray socket 會被誤繼承，
	// Accept 時以 accept4: invalid argument 讓 server 直接收攤（bugs.md #1）。
	if !fdIsListeningSocket(3) {
		return nil
	}

	file := os.NewFile(3, "listener")
	if file == nil {
		return nil
	}

	listener, err := net.FileListener(file)
	file.Close() // 無論成功與否，關閉 file descriptor 複本
	if err != nil {
		s.logger.Warningf("Failed to inherit listener: %v", err)
		return nil
	}

	// 驗證是 TCP listener
	if _, ok := listener.(*net.TCPListener); !ok {
		s.logger.Warning("Inherited listener is not a TCP listener, ignoring")
		listener.Close()
		return nil
	}

	s.logger.Info("Inherited listener from parent process")
	return listener
}

// handleShutdownSignals 監聽終止訊號（Ctrl+C / SIGTERM）並觸發優雅關閉。
// Shutdown 由他處先觸發時（shutdownChan 關閉）即結束監聽。
func (s *Server) handleShutdownSignals() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case <-quit:
		s.logger.Info("Received shutdown signal")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			s.logger.Warningf("Graceful shutdown: %v", err)
		}
	case <-s.shutdownChan:
	}
}

// Shutdown 優雅關閉伺服器（並行處理 HTTP/1+2 和 HTTP/3）。
// 冪等：重複呼叫不會 panic（v0.8.11 前二次呼叫會 close 已關閉的 channel），
// 首次之後的呼叫回傳 nil。
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() { err = s.doShutdown(ctx) })
	return err
}

// doShutdown 實際的關閉流程（僅由 shutdownOnce 執行一次）
func (s *Server) doShutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	s.shuttingDown.Store(true)

	// 注意：不手動 Close listener——http.Server.Shutdown 的第一步就是關閉
	// 所有 listener；搶先手動關會讓 Serve 的 Accept 以 net.ErrClosed 收場
	// （"use of closed network connection"），而非預期的 http.ErrServerClosed

	// 並行關閉 HTTP/1+2 和 HTTP/3 伺服器
	var httpErr, h3Err error
	done := make(chan struct{})

	go func() {
		if hs := s.httpServer.Load(); hs != nil {
			httpErr = hs.Shutdown(ctx)
		}
		if h3 := s.h3Server.Load(); h3 != nil {
			h3Err = h3.Close()
		}
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-ctx.Done():
		// 超時
		s.logger.Warning("Shutdown timed out, forcing close")
	}

	close(s.shutdownChan)
	s.removePIDFile()

	if httpErr != nil {
		return httpErr
	}
	return h3Err
}

// handleGracefulRestart 處理優雅重啟
func (s *Server) handleGracefulRestart() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, restartSignals...)
	defer signal.Stop(sigChan) // 確保清理信號訂閱

	for {
		select {
		case <-sigChan:
			s.logger.Info("Received graceful restart signal")

			// Fork 新進程
			child, err := s.forkNewProcess()
			if err != nil {
				s.logger.Emergencyf("Failed to fork new process: %v", err)
				continue
			}

			// 輪詢新進程存活狀態（每 200ms 最多 15 次 = 3 秒）。
			// 舊版只是空睡 3 秒、從不檢查任何狀態——若新進程啟動瞬間崩潰
			// （壞的執行檔、init 階段 panic 等），舊進程仍會照常自我關閉，
			// 造成兩個進程都不在、服務完全中斷。這裡改為真正輪詢：
			// 只要偵測到新進程已死，就放棄本次重啟、繼續留守，不自我關閉。
			if !s.waitForChildAlive(child, 15, 200*time.Millisecond) {
				s.logger.Emergencyf("New process (pid=%d) exited during startup; aborting restart, this process keeps serving", child.Pid)
				continue
			}

			// 優雅關閉
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.Shutdown(ctx); err != nil {
				s.logger.Emergencyf("Failed to shutdown gracefully: %v", err)
			}
			cancel()

			s.removePIDFile()
			os.Exit(0)

		case <-s.shutdownChan:
			return // Server 被正常關閉，退出 goroutine
		}
	}
}

// forkNewProcess 啟動新進程（修復 FD 洩漏）。回傳 *os.Process 供呼叫端輪詢存活狀態。
func (s *Server) forkNewProcess() (*os.Process, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	files := []*os.File{os.Stdin, os.Stdout, os.Stderr}

	// 傳遞監聽器檔案描述符
	if ln := s.getListenerAtomic(); ln != nil {
		if tcpListener, ok := ln.(*net.TCPListener); ok {
			file, err := tcpListener.File()
			if err == nil {
				files = append(files, file)
				defer file.Close() // 修復：fork 後關閉父程序的 FD 複本
			}
		}
	}

	attr := &os.ProcAttr{
		Env:   os.Environ(),
		Files: files,
	}

	process, err := os.StartProcess(executable, os.Args, attr)
	if err != nil {
		return nil, fmt.Errorf("failed to start new process: %w", err)
	}

	s.logger.Infof("Started new process with PID: %d", process.Pid)
	return process, nil
}

// waitForChildAlive 輪詢子進程是否仍存活，最多 attempts 次、每次間隔 interval。
// 用 signal 0（不送任何實際訊號，僅探測進程是否存在）逐次探測；只要有一次探測
// 失敗就視為子進程已結束，立即回傳 false（不必空等滿整個時長）。
// 全程存活則回傳 true。
func (s *Server) waitForChildAlive(child *os.Process, attempts int, interval time.Duration) bool {
	for i := 0; i < attempts; i++ {
		time.Sleep(interval)
		if err := child.Signal(syscall.Signal(0)); err != nil {
			return false
		}
	}
	return true
}

// applyDefaultMiddlewares 應用預設中間件
func (s *Server) applyDefaultMiddlewares() {
	if s.config.Server.Protocol == "http3" || s.config.Server.Protocol == "h3" {
		s.router.Use(middleware.HTTP3Middleware()...)
	} else {
		s.router.Use(middleware.DefaultMiddleware()...)
	}
}

// Health 健康檢查（使用 atomic 讀取避免競態）
func (s *Server) Health() error {
	if s.shuttingDown.Load() {
		return fmt.Errorf("server is shutting down")
	}

	if s.httpServer.Load() == nil && s.h3Server.Load() == nil {
		return fmt.Errorf("server not started")
	}

	return nil
}

// Static 服務靜態檔案
func (s *Server) Static(path string, dir string) {
	s.router.Static(path, dir)
}

// NotFound 設置 404 處理器（委派給 Router）
func (s *Server) NotFound(handler hypcontext.HandlerFunc) {
	s.router.NotFound(handler)
}

// MethodNotAllowed 設置 405 處理器（委派給 Router）
func (s *Server) MethodNotAllowed(handler hypcontext.HandlerFunc) {
	s.router.MethodNotAllowed(handler)
}

// GetProtocol 獲取當前協議
func (s *Server) GetProtocol() string {
	switch s.protocol {
	case HTTP3:
		return "HTTP/3"
	case HTTP2:
		return "HTTP/2"
	case HTTP1:
		return "HTTP/1.1"
	default:
		return "AUTO"
	}
}

// Manifest 生成應用程式的結構描述
func (s *Server) Manifest() *manifest.Manifest {
	c := manifest.NewCollector(s.router, s.config)
	return c.Collect()
}

// EnableHTTP3 啟用 HTTP/3
func (s *Server) EnableHTTP3(config *router.HTTP3Config) {
	s.router.EnableHTTP3(config)
}

// pidFilePath PID 檔案路徑（相對於 CWD，固定檔名 hypgo.pid 以維持既有腳本相容性）
const pidFilePath = "hypgo.pid"

// savePIDFile 保存 PID 檔案。寫入前偵測既有 PID 檔是否屬於另一個仍存活的行程
// （例如同一台機器、同一 CWD 誤起了兩個 hypgo 服務）——這種情況下直接覆寫會讓
// 監控／部署腳本讀到錯的 PID，因此改為大聲警告後才覆寫，不靜默吞掉。
// 注意：存活探測用 syscall.Signal(0)，Windows 上 (*os.Process).Signal 僅支援
// os.Kill/os.Interrupt，探測必定回錯——等同於偵測不到（維持原行為，非退化）。
func (s *Server) savePIDFile() error {
	if data, err := os.ReadFile(pidFilePath); err == nil {
		if oldPID, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil && oldPID != os.Getpid() {
			if proc, ferr := os.FindProcess(oldPID); ferr == nil && proc.Signal(syscall.Signal(0)) == nil {
				s.logger.Warningf("%s already belongs to running PID %d (another hypgo instance in this CWD?); overwriting — monitoring/deploy scripts may now track the wrong process", pidFilePath, oldPID)
			}
		}
	}

	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)
	return os.WriteFile(pidFilePath, []byte(pidStr), 0644)
}

// removePIDFile 移除 PID 檔案（只移除屬於自己的——避免誤刪另一個仍存活實例的 PID 檔）
func (s *Server) removePIDFile() {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		return
	}
	if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr != nil || pid != os.Getpid() {
		return
	}
	os.Remove(pidFilePath)
}

// isGracefulRestartEnabled 檢查是否啟用優雅重啟（讀 config.Server.EnableGracefulRestart）。
// Windows 一律停用：FD 3 繼承慣例在 Windows 不成立（子行程對同一位址 net.Listen
// 必因父行程仍佔用而失敗，重啟必以「子死、父退、服務消失」收場），且重啟訊號
// os.Interrupt 會劫持 Ctrl+C 的關閉語意。
func (s *Server) isGracefulRestartEnabled() bool {
	return runtime.GOOS != "windows" && s.config.Server.EnableGracefulRestart
}
