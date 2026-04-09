package grpc

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	cfg := Config{
		Addr:              ":0",
		EnableReflection:  true,
		EnableHealthCheck: true,
	}

	s := New(cfg, nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.grpcServer == nil {
		t.Fatal("grpcServer is nil")
	}
	if s.healthServer == nil {
		t.Fatal("healthServer should be created when EnableHealthCheck is true")
	}
}

func TestNewServerDefaults(t *testing.T) {
	s := New(Config{}, nil)
	if s.config.Addr != ":9090" {
		t.Errorf("default addr = %q, want :9090", s.config.Addr)
	}
}

func TestNewServerNoHealth(t *testing.T) {
	s := New(Config{EnableHealthCheck: false}, nil)
	if s.healthServer != nil {
		t.Error("healthServer should be nil when disabled")
	}
}

func TestServerStartStop(t *testing.T) {
	s := New(Config{Addr: ":0", EnableHealthCheck: true}, nil)

	// 啟動在背景
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	// 等 listener ready
	for s.listener == nil {
		// spin briefly
	}

	addr := s.Addr()
	if addr == "" {
		t.Error("Addr should be set after Start")
	}

	if s.IsShuttingDown() {
		t.Error("should not be shutting down")
	}

	s.GracefulStop()

	if !s.IsShuttingDown() {
		t.Error("should be shutting down after GracefulStop")
	}

	// 重複 GracefulStop 不應 panic
	s.GracefulStop()
}

func TestGRPCServer(t *testing.T) {
	s := New(Config{}, nil)
	if s.GRPCServer() == nil {
		t.Error("GRPCServer() should not return nil")
	}
}
