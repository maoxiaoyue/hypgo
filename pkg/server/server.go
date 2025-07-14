package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	//"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// Server represents the HTTP server
type Server struct {
	config     *config.Config
	router     *mux.Router
	httpServer *http.Server
	h3Server   *http3.Server
	logger     *logger.Logger
	listener   net.Listener
}

// New creates a new server instance
func New(cfg *config.Config, log *logger.Logger) *Server {
	return &Server{
		config: cfg,
		router: mux.NewRouter(),
		logger: log,
	}
}

// Start starts the server based on configured protocol
func (s *Server) Start() error {
	// Save PID file for hot restart
	if err := config.SavePIDFile(); err != nil {
		s.logger.Warning("Failed to save PID file: %v", err)
	}

	// Setup graceful restart handler if enabled
	if s.config.Server.EnableGracefulRestart {
		go s.handleGracefulRestart()
	}

	switch s.config.Server.Protocol {
	case "http3":
		return s.startHTTP3()
	case "http2":
		return s.startHTTP2()
	default:
		return s.startHTTP1()
	}
}

// startHTTP3 starts HTTP/3 server
func (s *Server) startHTTP3() error {
	s.logger.Info("Starting HTTP/3 server on %s", s.config.Server.Addr)

	if !s.config.Server.TLS.Enabled {
		return fmt.Errorf("HTTP/3 requires TLS to be enabled")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.loadCertificate()},
		NextProtos:   []string{"h3"},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}

	s.h3Server = &http3.Server{
		Handler:   s.router,
		Addr:      s.config.Server.Addr,
		TLSConfig: tlsConfig,
	}

	// Add Alt-Svc header for HTTP/3 discovery
	s.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Alt-Svc", fmt.Sprintf(`h3="%s"; ma=86400`, s.config.Server.Addr))
			next.ServeHTTP(w, r)
		})
	})

	return s.h3Server.ListenAndServe()
}

// startHTTP2 starts HTTP/2 server with HTTP/1.1 fallback
func (s *Server) startHTTP2() error {
	s.logger.Info("Starting HTTP/2 server with HTTP/1.1 fallback on %s", s.config.Server.Addr)

	h2s := &http2.Server{
		MaxHandlers:                  s.config.Server.MaxHandlers,
		MaxConcurrentStreams:         s.config.Server.MaxConcurrentStreams,
		MaxReadFrameSize:             s.config.Server.MaxReadFrameSize,
		PermitProhibitedCipherSuites: false,
		IdleTimeout:                  time.Duration(s.config.Server.IdleTimeout) * time.Second,
	}

	// Create listener for graceful restart support
	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener = listener

	handler := h2c.NewHandler(s.router, h2s)

	s.httpServer = &http.Server{
		Handler:           handler,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		}
		return s.httpServer.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	return s.httpServer.Serve(listener)
}

// startHTTP1 starts HTTP/1.1 server
func (s *Server) startHTTP1() error {
	s.logger.Info("Starting HTTP/1.1 server on %s", s.config.Server.Addr)

	// Create listener for graceful restart support
	listener, err := s.getListener()
	if err != nil {
		return err
	}
	s.listener = listener

	s.httpServer = &http.Server{
		Handler:           s.router,
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return s.httpServer.ServeTLS(listener, s.config.Server.TLS.CertFile, s.config.Server.TLS.KeyFile)
	}

	return s.httpServer.Serve(listener)
}

// getListener creates or inherits a listener
func (s *Server) getListener() (net.Listener, error) {
	// Check for inherited listener (for graceful restart)
	if ln := s.getInheritedListener(); ln != nil {
		return ln, nil
	}

	// Create new listener
	return net.Listen("tcp", s.config.Server.Addr)
}

// getInheritedListener checks for inherited listener from parent process
func (s *Server) getInheritedListener() net.Listener {
	// This is a simplified version. In production, you might want to use
	// github.com/cloudflare/tableflip or similar libraries
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

// loadCertificate loads TLS certificate
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

// Router returns the mux router
func (s *Server) Router() *mux.Router {
	return s.router
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")

	// Close listener to stop accepting new connections
	if s.listener != nil {
		s.listener.Close()
	}

	// Shutdown servers
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	if s.h3Server != nil {
		return s.h3Server.Close()
	}

	return nil
}

// handleGracefulRestart handles SIGUSR2 signal for graceful restart
func (s *Server) handleGracefulRestart() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR2)

	for {
		<-sigChan
		s.logger.Info("Received graceful restart signal")

		// Fork new process
		if err := s.forkNewProcess(); err != nil {
			s.logger.Emergency("Failed to fork new process: %v", err)
			continue
		}

		// Give new process time to start
		time.Sleep(2 * time.Second)

		// Graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			s.logger.Emergency("Failed to shutdown gracefully: %v", err)
		}

		// Remove PID file
		config.RemovePIDFile()

		os.Exit(0)
	}
}

// forkNewProcess starts a new process for graceful restart
func (s *Server) forkNewProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Prepare environment and file descriptors
	files := []*os.File{os.Stdin, os.Stdout, os.Stderr}

	// Pass listener file descriptor if exists
	if s.listener != nil {
		if tcpListener, ok := s.listener.(*net.TCPListener); ok {
			if file, err := tcpListener.File(); err == nil {
				files = append(files, file)
			}
		}
	}

	// Start new process
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

//ListenAndServeWithGracefulShutdown starts server with graceful shutdown support
/*
func (s *Server) ListenAndServeWithGracefulShutdown() error {
	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start()
	}()

	// Setup signal handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return err
	case <-quit:
		s.logger.Info("Received shutdown signal")

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			s.logger.Emergency("Server forced to shutdown: %v", err)
			return err
		}

		s.logger.Info("Server exited gracefully")
	}

	return nil
}*/

// Health returns server health status
func (s *Server) Health() error {
	// Check if server is running
	if s.httpServer == nil && s.h3Server == nil {
		return fmt.Errorf("server not started")
	}

	// You can add more health checks here
	// For example: database connectivity, memory usage, etc.

	return nil
}

// Middleware adds a middleware to the router
func (s *Server) Middleware(mw func(http.Handler) http.Handler) {
	s.router.Use(mux.MiddlewareFunc(mw))
}

// Static serves static files
func (s *Server) Static(path string, dir string) {
	s.router.PathPrefix(path).Handler(
		http.StripPrefix(path, http.FileServer(http.Dir(dir))),
	)
}

// NotFound sets the 404 handler
func (s *Server) NotFound(handler http.HandlerFunc) {
	s.router.NotFoundHandler = handler
}

// MethodNotAllowed sets the 405 handler
func (s *Server) MethodNotAllowed(handler http.HandlerFunc) {
	s.router.MethodNotAllowedHandler = handler
}
