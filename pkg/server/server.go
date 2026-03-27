// Package server 提供 HTTP/1.1/2/3 統一伺服器實現
package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/autosync"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
	"github.com/maoxiaoyue/hypgo/pkg/router"
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
	config     *config.Config
	router     *router.Router
	httpServer *http.Server
	h3Server   *http3.Server
	logger     *logger.Logger
	listener   net.Listener

	// 協議檢測
	protocol Protocol

	// 0-RTT 支援（帶 LRU 淘汰 + TTL）
	sessionCache *SessionCache

	// 優雅關閉（atomic 避免競態）
	shutdownChan chan struct{}
	shuttingDown atomic.Bool
}

// Protocol 協議類型
type Protocol int

const (
	HTTP1 Protocol = iota
	HTTP2
	HTTP3
	AUTO // 自動檢測
)

// sessionEntry 帶時間戳的 session 快取項
type sessionEntry struct {
	data      []byte
	createdAt time.Time
}

// SessionCache 0-RTT session 快取（帶大小上限 + TTL）
type SessionCache struct {
	entries map[string]sessionEntry
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
	return &SessionCache{
		entries: make(map[string]sessionEntry, 256),
		maxSize: defaultSessionCacheSize,
		ttl:     defaultSessionTTL,
	}
}

// Put 儲存 session，超過上限時淘汰最舊的條目
func (c *SessionCache) Put(key string, state []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 淘汰過期條目
	c.evictExpiredLocked()

	// 超過上限時淘汰最舊
	if len(c.entries) >= c.maxSize {
		c.evictOldestLocked()
	}

	c.entries[key] = sessionEntry{
		data:      state,
		createdAt: time.Now(),
	}
}

// GetAndDelete 原子性地取得並刪除 session（防止 0-RTT replay attack）
// 回傳 false 表示不存在或已過期
func (c *SessionCache) GetAndDelete(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// 檢查 TTL
	if time.Since(entry.createdAt) > c.ttl {
		delete(c.entries, key)
		return nil, false
	}

	// 原子 get-and-delete，防止 race window 重放
	delete(c.entries, key)
	return entry.data, true
}

// evictExpiredLocked 淘汰過期條目（必須持有鎖）
func (c *SessionCache) evictExpiredLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.createdAt) > c.ttl {
			delete(c.entries, key)
		}
	}
}

// evictOldestLocked 淘汰最舊的條目（必須持有鎖）
func (c *SessionCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, entry := range c.entries {
		if first || entry.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.createdAt
			first = false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
	}
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
		s.logger.Warning("Failed to save PID file: %v", err)
	}

	// AutoSync：啟動時自動同步 .hyp/context.yaml
	sync := autosync.New(
		autosync.Config{Enabled: true},
		s.router, s.config, s.logger,
	)
	sync.SyncSafe()

	// 設置優雅重啟處理
	if s.isGracefulRestartEnabled() {
		go s.handleGracefulRestart()
	}

	// 自動檢測協議或使用指定協議
	if s.config.Server.Protocol == "auto" {
		return s.startAutoProtocol()
	}

	switch s.config.Server.Protocol {
	case "http3", "h3":
		return s.startHTTP3()
	case "http2", "h2":
		return s.startHTTP2()
	default:
		return s.startHTTP1()
	}
}

// startAutoProtocol 自動協議選擇（同時支援 HTTP/1.1/2/3）
func (s *Server) startAutoProtocol() error {
	s.logger.Info("Starting server with auto protocol detection on %s", s.config.Server.Addr)

	// 啟動 HTTP/3 伺服器（UDP）
	if s.config.Server.TLS.Enabled {
		go func() {
			if err := s.startHTTP3(); err != nil {
				s.logger.Warning("HTTP/3 server failed: %v", err)
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
		s.sessionCache.Put(hex.EncodeToString(ticket), stateBytes)
		return ticket, nil
	}
}

// getTLSUnwrapSession 取得用於 TLS 1.3 0-RTT 的 UnwrapSession 函數
// 使用 GetAndDelete 原子操作防止 replay attack
func (s *Server) getTLSUnwrapSession() func([]byte, tls.ConnectionState) (*tls.SessionState, error) {
	return func(identity []byte, cs tls.ConnectionState) (*tls.SessionState, error) {
		key := hex.EncodeToString(identity)
		stateBytes, ok := s.sessionCache.GetAndDelete(key)
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
	s.logger.Info("Starting HTTP/3 server on %s", s.config.Server.Addr)

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

	// 創建 HTTP/3 伺服器
	s.h3Server = &http3.Server{
		Handler:         s.wrapH3Handler(),
		Addr:            s.config.Server.Addr,
		TLSConfig:       tlsConfig,
		EnableDatagrams: false,
		MaxHeaderBytes:  1 << 20,
	}

	// 監聽並服務
	return s.h3Server.ListenAndServe()
}

// startHTTP2WithFallback 啟動 HTTP/2 伺服器（支援 HTTP/1.1 降級）
func (s *Server) startHTTP2WithFallback() error {
	s.logger.Info("Starting HTTP/2 server with HTTP/1.1 fallback on %s", s.config.Server.Addr)

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
	s.listener = listener

	// 包裝處理器以支援協議檢測
	handler := s.wrapHandler(h2c.NewHandler(s.router, h2s))

	// 創建 HTTP 伺服器
	s.httpServer = &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// TLS 配置（統一 cipher suites）
	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			NextProtos:    []string{"h2", "http/1.1"},
			MinVersion:    tls.VersionTLS12,
			CipherSuites:  strongCipherSuites,
			WrapSession:   s.getTLSWrapSession(),
			UnwrapSession: s.getTLSUnwrapSession(),
		}
		return s.httpServer.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	return s.httpServer.Serve(listener)
}

// startHTTP2 啟動純 HTTP/2 伺服器
func (s *Server) startHTTP2() error {
	// 強制 HTTP/2
	s.protocol = HTTP2
	return s.startHTTP2WithFallback()
}

// startHTTP1 啟動 HTTP/1.1 伺服器
func (s *Server) startHTTP1() error {
	s.logger.Info("Starting HTTP/1.1 server on %s", s.config.Server.Addr)
	s.protocol = HTTP1

	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener = listener

	handler := s.wrapHandler(s.router)

	s.httpServer = &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// TLS 配置（統一 cipher suites）
	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			CipherSuites: strongCipherSuites,
		}
		return s.httpServer.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	return s.httpServer.Serve(listener)
}

// wrapHandler 包裝處理器以注入 Alt-Svc 標頭
func (s *Server) wrapHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.Server.TLS.Enabled && r.ProtoMajor < 3 {
			w.Header().Set("Alt-Svc", fmt.Sprintf(`h3="%s"; ma=86400`, s.config.Server.Addr))
		}
		h.ServeHTTP(w, r)
	})
}

// wrapH3Handler 包裝 HTTP/3 處理器
func (s *Server) wrapH3Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.router.ServeHTTP(w, r)
	})
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
	file := os.NewFile(3, "listener")
	if file == nil {
		return nil
	}

	listener, err := net.FileListener(file)
	file.Close() // 無論成功與否，關閉 file descriptor 複本
	if err != nil {
		s.logger.Warning("Failed to inherit listener: %v", err)
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

// Shutdown 優雅關閉伺服器（並行處理 HTTP/1+2 和 HTTP/3）
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	s.shuttingDown.Store(true)

	// 關閉監聽器（停止接受新連線）
	if s.listener != nil {
		s.listener.Close()
	}

	// 並行關閉 HTTP/1+2 和 HTTP/3 伺服器
	var httpErr, h3Err error
	done := make(chan struct{})

	go func() {
		if s.httpServer != nil {
			httpErr = s.httpServer.Shutdown(ctx)
		}
		if s.h3Server != nil {
			h3Err = s.h3Server.Close()
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
			if err := s.forkNewProcess(); err != nil {
				s.logger.Emergency("Failed to fork new process: %v", err)
				continue
			}

			// 等待新進程啟動（poll 方式，每 200ms 最多 15 次 = 3 秒）
			for i := 0; i < 15; i++ {
				time.Sleep(200 * time.Millisecond)
			}

			// 優雅關閉
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.Shutdown(ctx); err != nil {
				s.logger.Emergency("Failed to shutdown gracefully: %v", err)
			}
			cancel()

			s.removePIDFile()
			os.Exit(0)

		case <-s.shutdownChan:
			return // Server 被正常關閉，退出 goroutine
		}
	}
}

// forkNewProcess 啟動新進程（修復 FD 洩漏）
func (s *Server) forkNewProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	files := []*os.File{os.Stdin, os.Stdout, os.Stderr}

	// 傳遞監聽器檔案描述符
	if s.listener != nil {
		if tcpListener, ok := s.listener.(*net.TCPListener); ok {
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
		return fmt.Errorf("failed to start new process: %w", err)
	}

	s.logger.Info("Started new process with PID: %d", process.Pid)
	return nil
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

	if s.httpServer == nil && s.h3Server == nil {
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

// savePIDFile 保存 PID 檔案
func (s *Server) savePIDFile() error {
	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)
	return os.WriteFile("hypgo.pid", []byte(pidStr), 0644)
}

// removePIDFile 移除 PID 檔案
func (s *Server) removePIDFile() {
	os.Remove("hypgo.pid")
}

// isGracefulRestartEnabled 檢查是否啟用優雅重啟
func (s *Server) isGracefulRestartEnabled() bool {
	return true
}
