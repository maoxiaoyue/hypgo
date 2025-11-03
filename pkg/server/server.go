// Package server 提供 HTTP/1.1/2/3 統一伺服器實現
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

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

	// 0-RTT 支援
	sessionCache *SessionCache

	// 優雅關閉
	shutdownChan   chan struct{}
	isShuttingDown bool
}

// Protocol 協議類型
type Protocol int

const (
	HTTP1 Protocol = iota
	HTTP2
	HTTP3
	AUTO // 自動檢測
)

// SessionCache 0-RTT session 快取
type SessionCache struct {
	cache map[string][]byte
}

// New 創建新的伺服器實例
func New(cfg *config.Config, log *logger.Logger) *Server {
	return &Server{
		config:       cfg,
		router:       router.New(),
		logger:       log,
		sessionCache: &SessionCache{cache: make(map[string][]byte)},
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

// startHTTP3 啟動 HTTP/3 伺服器
func (s *Server) startHTTP3() error {
	s.logger.Info("Starting HTTP/3 server on %s", s.config.Server.Addr)

	if !s.config.Server.TLS.Enabled {
		return fmt.Errorf("HTTP/3 requires TLS to be enabled")
	}

	// 配置 TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.loadCertificate()},
		NextProtos:   []string{"h3"},
		MinVersion:   tls.VersionTLS13,
	}

	// 創建 HTTP/3 伺服器
	s.h3Server = &http3.Server{
		Handler:   s.wrapH3Handler(),
		Addr:      s.config.Server.Addr,
		TLSConfig: tlsConfig,
		// QUIC 配置通過 TLSConfig 和其他方式設置
		EnableDatagrams: false,
		MaxHeaderBytes:  1 << 20,
	}

	// 監聽並服務
	return s.h3Server.ListenAndServe()
}

// startHTTP2WithFallback 啟動 HTTP/2 伺服器（支援 HTTP/1.1 降級）
func (s *Server) startHTTP2WithFallback() error {
	s.logger.Info("Starting HTTP/2 server with HTTP/1.1 fallback on %s", s.config.Server.Addr)

	// 配置 HTTP/2
	h2s := &http2.Server{
		MaxHandlers:                  s.config.Server.MaxHandlers,
		MaxConcurrentStreams:         uint32(s.config.Server.MaxConcurrentStreams),
		MaxReadFrameSize:             uint32(s.config.Server.MaxReadFrameSize),
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

	// TLS 配置
	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
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

	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return s.httpServer.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	return s.httpServer.Serve(listener)
}

// wrapHandler 包裝處理器以注入協議資訊
func (s *Server) wrapHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 創建 HypGo Context
		c := hypcontext.New(w, r)
		defer c.Release()

		// 設置協議資訊
		protocol := s.detectProtocol(r)
		c.Set("protocol", protocol)

		// 如果是 HTTP/3，添加 Alt-Svc 頭
		if s.config.Server.TLS.Enabled && protocol != "HTTP/3" {
			w.Header().Set("Alt-Svc", fmt.Sprintf(`h3="%s"; ma=86400`, s.config.Server.Addr))
		}

		// 處理請求
		h.ServeHTTP(w, r)
	})
}

// wrapH3Handler 包裝 HTTP/3 處理器
func (s *Server) wrapH3Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 創建 HypGo Context
		c := hypcontext.New(w, r)
		defer c.Release()

		// 設置 HTTP/3 協議資訊
		c.Set("protocol", "HTTP/3")

		// 處理請求
		s.router.ServeHTTP(w, r)
	})
}

// detectProtocol 檢測請求使用的協議
func (s *Server) detectProtocol(r *http.Request) string {
	// 檢查請求的協議版本
	if r.ProtoMajor == 3 {
		return "HTTP/3"
	} else if r.ProtoMajor == 2 {
		return "HTTP/2"
	}
	return "HTTP/1.1"
}

// getListener 創建或繼承監聽器
func (s *Server) getListener() (net.Listener, error) {
	// 檢查繼承的監聽器（優雅重啟）
	if ln := s.getInheritedListener(); ln != nil {
		return ln, nil
	}

	// 創建新監聽器
	return net.Listen("tcp", s.config.Server.Addr)
}

// getInheritedListener 獲取繼承的監聽器
func (s *Server) getInheritedListener() net.Listener {
	file := os.NewFile(3, "listener")
	if file == nil {
		return nil
	}

	listener, err := net.FileListener(file)
	if err != nil {
		s.logger.Warning("Failed to inherit listener: %v", err)
		return nil
	}

	s.logger.Info("Inherited listener from parent process")
	return listener
}

// loadCertificate 載入 TLS 證書
func (s *Server) loadCertificate() tls.Certificate {
	cert, err := tls.LoadX509KeyPair(
		s.config.Server.TLS.CertFile,
		s.config.Server.TLS.KeyFile,
	)
	if err != nil {
		s.logger.Emergency("Failed to load TLS certificate: %v", err)
		panic(err)
	}
	return cert
}

// Shutdown 優雅關閉伺服器
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	s.isShuttingDown = true

	// 關閉監聽器
	if s.listener != nil {
		s.listener.Close()
	}

	// 關閉伺服器
	var err error
	if s.httpServer != nil {
		err = s.httpServer.Shutdown(ctx)
	}
	if s.h3Server != nil {
		if e := s.h3Server.Close(); e != nil && err == nil {
			err = e
		}
	}

	close(s.shutdownChan)
	return err
}

// handleGracefulRestart 處理優雅重啟
func (s *Server) handleGracefulRestart() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR2)

	for {
		<-sigChan
		s.logger.Info("Received graceful restart signal")

		// Fork 新進程
		if err := s.forkNewProcess(); err != nil {
			s.logger.Emergency("Failed to fork new process: %v", err)
			continue
		}

		// 等待新進程啟動
		time.Sleep(2 * time.Second)

		// 優雅關閉
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			s.logger.Emergency("Failed to shutdown gracefully: %v", err)
		}

		// 移除 PID 檔案
		s.removePIDFile()
		os.Exit(0)
	}
}

// forkNewProcess 啟動新進程
func (s *Server) forkNewProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	files := []*os.File{os.Stdin, os.Stdout, os.Stderr}

	// 傳遞監聽器檔案描述符
	if s.listener != nil {
		if tcpListener, ok := s.listener.(*net.TCPListener); ok {
			if file, err := tcpListener.File(); err == nil {
				files = append(files, file)
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
	// 根據協議選擇中間件組
	if s.config.Server.Protocol == "http3" || s.config.Server.Protocol == "h3" {
		// HTTP/3 優化中間件
		s.router.Use(middleware.HTTP3Middleware()...)
	} else {
		// 標準中間件
		s.router.Use(middleware.DefaultMiddleware()...)
	}
}

// Health 健康檢查
func (s *Server) Health() error {
	if s.isShuttingDown {
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

// NotFound 設置 404 處理器
func (s *Server) NotFound(handler hypcontext.HandlerFunc) {
	// s.router.NotFoundHandler = handler
	// 這需要在 router 中實現
}

// MethodNotAllowed 設置 405 處理器
func (s *Server) MethodNotAllowed(handler hypcontext.HandlerFunc) {
	// s.router.MethodNotAllowedHandler = handler
	// 這需要在 router 中實現
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
	// 預設啟用優雅重啟
	return true
}
