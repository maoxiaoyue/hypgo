package server

import (
	"context"
	"crypto/tls"
	//"fmt"
	//"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Server struct {
	config     *config.Config
	router     *mux.Router
	httpServer *http.Server
	h3Server   *http3.Server
	logger     *logger.Logger
}

func New(cfg *config.Config, log *logger.Logger) *Server {
	return &Server{
		config: cfg,
		router: mux.NewRouter(),
		logger: log,
	}
}

func (s *Server) Start() error {
	switch s.config.Server.Protocol {
	case "http3":
		return s.startHTTP3()
	case "http2":
		return s.startHTTP2()
	default:
		return s.startHTTP1()
	}
}

func (s *Server) startHTTP3() error {
	s.logger.Info("Starting HTTP/3 server on %s", s.config.Server.Addr)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.loadCertificate()},
		NextProtos:   []string{"h3"},
	}

	s.h3Server = &http3.Server{
		Handler:   s.router,
		Addr:      s.config.Server.Addr,
		TLSConfig: tlsConfig,
		QuicConfig: &quic.Config{
			MaxIdleTimeout:  time.Duration(s.config.Server.IdleTimeout) * time.Second,
			KeepAlivePeriod: time.Duration(s.config.Server.KeepAlive) * time.Second,
		},
	}

	return s.h3Server.ListenAndServe()
}

func (s *Server) startHTTP2() error {
	s.logger.Info("Starting HTTP/2 server with HTTP/1.1 fallback on %s", s.config.Server.Addr)

	h2s := &http2.Server{
		MaxHandlers:                  s.config.Server.MaxHandlers,
		MaxConcurrentStreams:         s.config.Server.MaxConcurrentStreams,
		MaxReadFrameSize:             s.config.Server.MaxReadFrameSize,
		PermitProhibitedCipherSuites: false,
	}

	handler := h2c.NewHandler(s.router, h2s)

	s.httpServer = &http.Server{
		Addr:         s.config.Server.Addr,
		Handler:      handler,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.config.Server.IdleTimeout) * time.Second,
	}

	if s.config.Server.TLS.Enabled {
		s.httpServer.TLSConfig = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
		}
		return s.httpServer.ListenAndServeTLS(
			s.config.Server.TLS.CertFile,
			s.config.Server.TLS.KeyFile,
		)
	}

	return s.httpServer.ListenAndServe()
}

func (s *Server) startHTTP1() error {
	s.logger.Info("Starting HTTP/1.1 server on %s", s.config.Server.Addr)

	s.httpServer = &http.Server{
		Addr:         s.config.Server.Addr,
		Handler:      s.router,
		ReadTimeout:  time.Duration(s.config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.config.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.config.Server.IdleTimeout) * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

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

func (s *Server) Router() *mux.Router {
	return s.router
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	if s.h3Server != nil {
		return s.h3Server.Close()
	}
	return nil
}
