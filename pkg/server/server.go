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
	// udpConn HTTP/3 的 UDP socket（net.PacketConn）。http3.Server.Serve 關閉時不會關閉它，由 doShutdown 關閉
	udpConn atomic.Value

	// 0-RTT 支援（帶 LRU 淘汰 + TTL）
	sessionCache *SessionCache

	// altSvc 目前廣告的 Alt-Svc 值（nil = 不廣告）：auto 模式下 HTTP/3 的 UDP socket 綁定成功後才設定
	altSvc atomic.Pointer[string]

	// handlerSem 限制同時執行的 handler 數（config.Server.MaxHandlers；nil = 不限）
	handlerSem chan struct{}

	// restarted 本行程是否由優雅重啟產生（HTTP/3 需等父行程釋放 UDP 埠）
	restarted bool

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

// restartEnv 優雅重啟時設給子行程的環境變數
const restartEnv = "HYPGO_RESTARTED"

// restartBindTimeout 重啟產生的子行程等待父行程釋放 HTTP/3 UDP 埠的上限
// （父行程的優雅關閉最長 30 秒，另留餘裕）
const restartBindTimeout = 60 * time.Second

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
	// hyp context 以 HYPGO_MANIFEST_OUT 執行專案：此時路由已註冊完，寫出 manifest 後即返回，
	// 不寫 PID 檔、不開始監聽，也不動 .hyp/context.yaml
	if out := os.Getenv(manifest.ManifestOutEnv); out != "" {
		return manifest.NewAutoSync(manifest.AutoSyncConfig{
			Enabled: true, Path: out, Format: "json", DisableLLMAutoDiscovery: true,
		}, s.router, s.config, s.logger).Sync()
	}

	s.restarted = os.Getenv(restartEnv) == "1"
	os.Unsetenv(restartEnv) // 不再傳給應用程式自行啟動的子行程

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

	// AutoSync：啟動時自動同步 .hyp/context.yaml（server.disable_auto_sync 可關閉；
	// 設定了 LLM 時於背景執行，不卡住啟動）
	manifest.NewAutoSync(
		manifest.AutoSyncConfig{Enabled: !s.config.Server.DisableAutoSync},
		s.router, s.config, s.logger,
	).Run()

	// 套用預設中間件（Recovery / Logger / RejectEarlyData / Security / CORS；http3 時為 HTTP3Middleware）。
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

	// handler 並發上限：x/net 的 http2.Server.MaxHandlers 標註 TODO 未實作，
	// 先前把設定傳給它，max_handlers 對任何協議都沒有效果
	if n := s.config.Server.MaxHandlers; n > 0 {
		s.handlerSem = make(chan struct{}, n)
	}

	var err error
	switch s.config.Server.Protocol {
	case "auto":
		err = s.startAutoProtocol(false)
	case "http3", "h3":
		err = s.startAutoProtocol(true)
	case "http2", "h2":
		err = s.startHTTP2()
	default:
		err = s.startHTTP1()
	}

	// 優雅關閉時 Serve/ListenAndServe 依 stdlib 慣例回傳 http.ErrServerClosed
	// （quic-go http3 v0.59+ 亦同）——屬預期流程而非錯誤，過濾掉，
	// 避免呼叫端（main.go / App.Run）在正常關閉時誤報 Server error。
	// net.ErrClosed 同理：關閉流程中 listener 先一步關掉時 Accept 的收場錯誤
	if errors.Is(err, http.ErrServerClosed) || (errors.Is(err, net.ErrClosed) && s.shuttingDown.Load()) {
		return nil
	}
	if err != nil && !s.shuttingDown.Load() {
		// 啟動失敗（如埠被占用）：移除 PID 檔並停止訊號監聽。先前 PID 檔留在原地，
		// 且 signal.Notify 持續攔截 SIGINT / SIGTERM——之後按 Ctrl+C 不會結束行程
		s.Shutdown(context.Background())
	}
	return err
}

// startAutoProtocol 同時啟動 HTTP/3（UDP）與 HTTP/1.1 + HTTP/2（TCP，回應帶 Alt-Svc 讓瀏覽器升級）。
// h3Required（protocol: http3）時 HTTP/3 起不來就整個 Start 失敗；auto 只記警告、照常服務 TCP。
// 先前 http3 模式只監聽 UDP：瀏覽器沒有 TCP 回應可以讀到 Alt-Svc，無從發現 HTTP/3
func (s *Server) startAutoProtocol(h3Required bool) error {
	s.logger.Infof("Starting server with auto protocol detection on %s", s.config.Server.Addr)

	if !s.config.Server.TLS.Enabled {
		if h3Required {
			return fmt.Errorf("HTTP/3 requires TLS to be enabled")
		}
		return s.startHTTP2WithFallback()
	}

	h3Err := make(chan error, 1)
	go func() {
		err := s.startHTTP3()
		if err == nil || errors.Is(err, http.ErrServerClosed) || s.shuttingDown.Load() {
			return
		}
		if !h3Required {
			s.logger.Warningf("HTTP/3 server failed: %v", err)
			return
		}
		h3Err <- err
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()

	err := s.startHTTP2WithFallback()
	select {
	case e := <-h3Err:
		return e
	default:
		return err
	}
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

	// 自行綁定 UDP 再 Serve（quic-go 建議：ListenAndServe 與 Shutdown 併用可能競爭），
	// 也才知道綁定是否成功、實際的埠號
	pc, err := s.listenUDP()
	if err != nil {
		return err
	}
	s.udpConn.Store(pc)

	// 創建 HTTP/3 伺服器
	h3srv := &http3.Server{
		Handler:         s.wrapHandler(s.router, false),
		Addr:            s.config.Server.Addr,
		TLSConfig:       tlsConfig,
		QUICConfig:      quicConf,
		EnableDatagrams: false,
		MaxHeaderBytes:  1 << 20,
		// 讓 c.IsEarlyData() 能分辨 0-RTT 請求：QUICConfig 開了 Allow0RTT，
		// quic-go 收到 0-RTT 請求即派發，不等握手完成
		ConnContext: func(ctx context.Context, c *quic.Conn) context.Context {
			return hypcontext.WithHandshakeComplete(ctx, c.HandshakeComplete())
		},
	}
	s.h3Server.Store(h3srv)
	if s.shuttingDown.Load() { // 理由同 serveTCP
		pc.Close()
		return http.ErrServerClosed
	}

	// UDP 綁定成功才在 TCP 回應廣告 Alt-Svc，且只帶埠號：先前 HTTP/3 啟動失敗
	// 仍每個回應都廣告，主機也直接取 Addr——綁 0.0.0.0 時叫客戶端連往 0.0.0.0
	v := fmt.Sprintf(`h3=":%d"; ma=86400`, pc.LocalAddr().(*net.UDPAddr).Port)
	s.altSvc.Store(&v)
	return h3srv.Serve(pc)
}

// listenUDP 綁定 HTTP/3 的 UDP socket。優雅重啟產生的子行程在父行程釋放埠之前會綁定失敗：
// 持續重試到成功或 restartBindTimeout，期間不廣告 Alt-Svc（客戶端走 TCP）。
// 先前子行程只試一次即放棄，重啟後 HTTP/3 永久消失。不與父行程共用同一個 UDP socket：
// 核心會把封包任意分給兩個行程，交接期間父行程的 QUIC 連線會大量掉封包
func (s *Server) listenUDP() (net.PacketConn, error) {
	pc, err := net.ListenPacket("udp", s.config.Server.Addr)
	if err == nil || !s.restarted {
		return pc, err
	}
	s.logger.Infof("HTTP/3 port busy (%v); waiting for the previous process to release it", err)
	deadline := time.Now().Add(restartBindTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-s.shutdownChan:
			return nil, http.ErrServerClosed
		case <-time.After(200 * time.Millisecond):
		}
		if pc, err = net.ListenPacket("udp", s.config.Server.Addr); err == nil {
			s.logger.Info("HTTP/3 port bound after the previous process released it")
			return pc, nil
		}
	}
	return nil, err
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
	handler = s.wrapHandler(handler, true)

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
	}
	return s.serveTCP(httpSrv, listener)
}

// startHTTP2 啟動 HTTP/2 伺服器（同 startHTTP2WithFallback）
func (s *Server) startHTTP2() error {
	return s.startHTTP2WithFallback()
}

// startHTTP1 啟動 HTTP/1.1 伺服器
func (s *Server) startHTTP1() error {
	s.logger.Infof("Starting HTTP/1.1 server on %s", s.config.Server.Addr)

	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener.Store(listener)

	handler := s.wrapHandler(s.router, true)

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
			NextProtos:   []string{"http/1.1"},
			MinVersion:   tls.VersionTLS12,
			CipherSuites: strongCipherSuites,
		}
		// 非 nil 的空 map 停用 net/http 自動啟用的 h2：先前 protocol: http1 加上 TLS 仍協商出 HTTP/2
		httpSrv.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	}
	return s.serveTCP(httpSrv, listener)
}

// serveTCP 記錄 httpServer 後開始服務。記錄之後再檢查 shuttingDown：Shutdown 若在記錄之前
// 就讀到 nil（HTTP/3 啟動失敗、啟動中收到訊號），這裡會看到旗標並先關閉，否則 TCP 會在
// Shutdown 之後繼續服務。關閉後的 Serve 立即回傳 http.ErrServerClosed 並關掉 listener
func (s *Server) serveTCP(srv *http.Server, l net.Listener) error {
	s.httpServer.Store(srv)
	if s.shuttingDown.Load() {
		srv.Close()
	}
	if s.config.Server.TLS.Enabled {
		return srv.ServeTLS(l, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}
	return srv.Serve(l)
}

// wrapHandler 最外層包裝：限制同時執行的 handler 數（MaxHandlers），
// TCP 側另在 HTTP/3 可用時加上 Alt-Svc。兩者都不需要時不加包裝層
func (s *Server) wrapHandler(h http.Handler, tcp bool) http.Handler {
	// altSvc 只在 HTTP/3 綁定後才有值（auto / http3 模式），其餘模式讀到 nil 不廣告
	advertise := tcp && s.config.Server.TLS.Enabled
	sem := s.handlerSem
	if !advertise && sem == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if advertise {
			if v := s.altSvc.Load(); v != nil {
				w.Header().Set("Alt-Svc", *v)
			}
		}
		if sem != nil {
			// 達上限時等待空位；客戶端取消請求即放棄
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-r.Context().Done():
				return
			}
		}
		h.ServeHTTP(w, r)
	})
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

// doShutdown 實際的關閉流程（僅由 shutdownOnce 執行一次）：HTTP/1+2 與 HTTP/3 並行優雅關閉，
// ctx 到期時強制關閉剩餘連線。先前逾時只記一行 "forcing close"、連線照樣跑完；HTTP/3 以 Close
// 直接中斷且要等 TCP 側關完才開始；逾時後讀取錯誤變數與背景 goroutine 的寫入有 data race
func (s *Server) doShutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	s.shuttingDown.Store(true)

	// 注意：不手動 Close listener——http.Server.Shutdown 的第一步就是關閉
	// 所有 listener；搶先手動關會讓 Serve 的 Accept 以 net.ErrClosed 收場
	// （"use of closed network connection"），而非預期的 http.ErrServerClosed
	hs, h3 := s.httpServer.Load(), s.h3Server.Load()
	errs := make(chan error, 2)
	pending := 0
	if hs != nil {
		pending++
		go func() { errs <- hs.Shutdown(ctx) }()
	}
	if h3 != nil {
		pending++
		go func() { errs <- h3.Shutdown(ctx) }() // 送 GOAWAY 等請求完成；ctx 到期時自行強制關閉
	}
	var err error
	for ; pending > 0; pending-- {
		if e := <-errs; e != nil && err == nil {
			err = e
		}
	}
	if ctx.Err() != nil {
		s.logger.Warning("Shutdown timed out, forcing close")
		if hs != nil {
			hs.Close()
		}
	}
	if pc, ok := s.udpConn.Load().(net.PacketConn); ok {
		pc.Close()
	}

	close(s.shutdownChan)
	s.removePIDFile()
	return err
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

			// 新進程在 3 秒內結束（壞的執行檔、init 階段 panic 等）就放棄本次重啟、繼續留守，
			// 不自我關閉——否則兩個進程都不在、服務完全中斷
			if !s.waitForChildAlive(child, 3*time.Second) {
				s.logger.Emergencyf("New process (pid=%d) exited during startup; aborting restart, this process keeps serving", child.Pid)
				continue
			}

			// 優雅關閉：Start() 隨之返回 nil，由 main 正常結束。先前直接 os.Exit(0)，
			// 跳過 main 裡的 defer（如 db.Close()）
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.Shutdown(ctx); err != nil {
				s.logger.Emergencyf("Failed to shutdown gracefully: %v", err)
			}
			cancel()
			return

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

	// 標記子行程由重啟產生（HTTP/3 需等本行程釋放 UDP 埠）
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, restartEnv+"=") {
			env = append(env, kv)
		}
	}
	attr := &os.ProcAttr{
		Env:   append(env, restartEnv+"=1"),
		Files: files,
	}

	process, err := os.StartProcess(executable, os.Args, attr)
	if err != nil {
		return nil, fmt.Errorf("failed to start new process: %w", err)
	}

	s.logger.Infof("Started new process with PID: %d", process.Pid)
	return process, nil
}

// waitForChildAlive 等待 window；子進程在此期間結束即回傳 false。
// 以 Wait 判斷（跨平台準確）：先前以 Signal(0) 探測，但在 Unix 上已結束、尚未被 Wait 的子進程
// 是 zombie，Signal(0) 照樣成功——啟動即崩潰的子進程被當成存活，父進程照常自我關閉
func (s *Server) waitForChildAlive(child *os.Process, window time.Duration) bool {
	exited := make(chan struct{})
	go func() {
		child.Wait()
		close(exited)
	}()
	select {
	case <-exited:
		return false
	case <-time.After(window):
		return true
	}
}

// applyDefaultMiddlewares 應用預設中間件
func (s *Server) applyDefaultMiddlewares() {
	// 請求日誌與 panic 記錄走 server logger（遵守日誌等級與 JSON 格式），不再直接寫 stdout
	access, errs := logWriter(s.logger.Info), logWriter(s.logger.Error)
	if s.config.Server.Protocol == "http3" || s.config.Server.Protocol == "h3" {
		s.router.Use(middleware.HTTP3MiddlewareWithOutput(access, errs)...)
	} else {
		s.router.Use(middleware.DefaultMiddlewareWithOutput(access, errs)...)
	}
}

// logWriter 把中間件寫出的每一行轉成一筆 logger 紀錄
type logWriter func(msg string, keysAndValues ...interface{})

func (f logWriter) Write(p []byte) (int, error) {
	f(strings.TrimRight(string(p), "\n"))
	return len(p), nil
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

// GetProtocol 回傳設定的協議（"HTTP/1.1" / "HTTP/2" / "HTTP/3" / "AUTO"）。
// 先前讀取 Start 內寫入的欄位：auto 與 http3 模式從未設定，零值使兩者都回報 HTTP/1.1
func (s *Server) GetProtocol() string {
	switch s.config.Server.Protocol {
	case "http3", "h3":
		return "HTTP/3"
	case "http2", "h2":
		return "HTTP/2"
	case "auto":
		return "AUTO"
	default:
		return "HTTP/1.1"
	}
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
