// Package grpc 提供 gRPC Server 封裝
// 統一管理 listener、interceptor、reflection、health check、graceful shutdown
package grpc

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Config gRPC Server 配置
type Config struct {
	// Addr 監聽地址（如 ":9090"）
	Addr string

	// TLS 配置（nil 表示不使用 TLS）
	TLS *TLSConfig

	// EnableReflection 是否啟用 gRPC reflection（方便 grpcurl 偵錯）
	EnableReflection bool

	// EnableHealthCheck 是否啟用 gRPC 健康檢查服務
	EnableHealthCheck bool

	// MaxRecvMsgSize 最大接收訊息大小（bytes，預設 4MB）
	MaxRecvMsgSize int

	// MaxSendMsgSize 最大發送訊息大小（bytes，預設 4MB）
	MaxSendMsgSize int
}

// TLSConfig TLS 配置
type TLSConfig struct {
	CertFile string
	KeyFile  string
}

// Server gRPC 伺服器封裝
type Server struct {
	config       Config
	grpcServer   *grpc.Server
	logger       *logger.Logger
	listener     net.Listener
	healthServer *health.Server
	shuttingDown atomic.Bool
}

// New 建立新的 gRPC Server
func New(cfg Config, log *logger.Logger, opts ...grpc.ServerOption) *Server {
	s := &Server{
		config: cfg,
		logger: log,
	}

	// 預設值
	if s.config.Addr == "" {
		s.config.Addr = ":9090"
	}

	// 組裝 server options
	serverOpts := make([]grpc.ServerOption, 0, len(opts)+4)

	// TLS
	if cfg.TLS != nil && cfg.TLS.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			log.Error("Failed to load TLS certificate: %v", err)
		} else {
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
			}
			serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		}
	}

	// 訊息大小限制
	if cfg.MaxRecvMsgSize > 0 {
		serverOpts = append(serverOpts, grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize))
	}
	if cfg.MaxSendMsgSize > 0 {
		serverOpts = append(serverOpts, grpc.MaxSendMsgSize(cfg.MaxSendMsgSize))
	}

	// 使用者自訂 options（interceptor 等）
	serverOpts = append(serverOpts, opts...)

	s.grpcServer = grpc.NewServer(serverOpts...)

	// Reflection（grpcurl 偵錯用）
	if cfg.EnableReflection {
		reflection.Register(s.grpcServer)
	}

	// Health check
	if cfg.EnableHealthCheck {
		s.healthServer = health.NewServer()
		healthpb.RegisterHealthServer(s.grpcServer, s.healthServer)
	}

	return s
}

// GRPCServer 返回底層 grpc.Server（用於註冊服務）
func (s *Server) GRPCServer() *grpc.Server {
	return s.grpcServer
}

// SetServingStatus 設定服務健康狀態（需啟用 EnableHealthCheck）
func (s *Server) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	if s.healthServer != nil {
		s.healthServer.SetServingStatus(service, status)
	}
}

// Start 啟動 gRPC Server（阻塞）
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("grpc: failed to listen on %s: %w", s.config.Addr, err)
	}
	s.listener = lis

	if s.logger != nil {
		protocol := "plaintext"
		if s.config.TLS != nil {
			protocol = "TLS"
		}
		s.logger.Info("gRPC server listening on %s (%s)", s.config.Addr, protocol)
	}

	// 標記所有服務為 SERVING
	if s.healthServer != nil {
		s.healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	}

	return s.grpcServer.Serve(lis)
}

// GracefulStop 優雅關閉（等待進行中的 RPC 完成）
func (s *Server) GracefulStop() {
	if s.shuttingDown.Swap(true) {
		return // 已在關閉中
	}

	if s.logger != nil {
		s.logger.Info("gRPC server shutting down gracefully...")
	}

	// 標記為 NOT_SERVING
	if s.healthServer != nil {
		s.healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	}

	s.grpcServer.GracefulStop()

	if s.logger != nil {
		s.logger.Info("gRPC server stopped")
	}
}

// Stop 立即停止（不等待）
func (s *Server) Stop() {
	s.shuttingDown.Store(true)
	s.grpcServer.Stop()
}

// IsShuttingDown 是否正在關閉
func (s *Server) IsShuttingDown() bool {
	return s.shuttingDown.Load()
}

// Addr 返回實際監聽地址（Start 後有效）
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.Addr
}
