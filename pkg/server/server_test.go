package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

func TestNewServer(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	log := logger.NewLogger()

	s := New(&cfg, log)

	if s == nil {
		t.Errorf("New() returned nil")
	}
	if s.config != &cfg {
		t.Errorf("Config pointer mismatch")
	}
	if s.logger != log {
		t.Errorf("Logger pointer mismatch")
	}
	if s.router == nil {
		t.Errorf("Router not initialized")
	}
}

func TestServerRouter(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	r := s.Router()
	if r == nil {
		t.Errorf("Router() returned nil")
	}
	if r != s.router {
		t.Errorf("Router returned is not the same instance")
	}
}

func TestServerHealth(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// Add health endpoint
	s.Health()

	// test health before starting
	err := s.Health()
	if err == nil {
		t.Errorf("Expected error from Health when not started")
	}

	// Fake start
	s.httpServer = &http.Server{}
	if err := s.Health(); err != nil {
		t.Errorf("Expected no error from Health when started, got: %v", err)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	// Use an ephemeral port for testing
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	go func() {
		err := s.Start()
		// Start() could return net.ErrClosed intentionally, don't fail unless it's a real unexpected error.
		if err != nil && err != http.ErrServerClosed {
			t.Logf("Start() returned an error (which might be expected upon shutdown): %v", err)
		}
	}()

	// Wait for server to bind port
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned: %v", err)
	}
}

func TestProtocolGetter(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	s.protocol = HTTP1
	if p := s.GetProtocol(); p != "HTTP/1.1" {
		t.Errorf("Expected HTTP/1.1, got %s", p)
	}

	s.protocol = HTTP2
	if p := s.GetProtocol(); p != "HTTP/2" {
		t.Errorf("Expected HTTP/2, got %s", p)
	}

	s.protocol = HTTP3
	if p := s.GetProtocol(); p != "HTTP/3" {
		t.Errorf("Expected HTTP/3, got %s", p)
	}
}
