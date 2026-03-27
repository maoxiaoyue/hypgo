package server

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
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
	if s.sessionCache == nil {
		t.Errorf("SessionCache not initialized")
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

	// Simulate shutdown
	s.shuttingDown.Store(true)
	if err := s.Health(); err == nil {
		t.Errorf("Expected error from Health when shutting down")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	go func() {
		err := s.Start()
		if err != nil && err != http.ErrServerClosed {
			t.Logf("Start() returned: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned: %v", err)
	}

	// 確認 shuttingDown 已設置
	if !s.shuttingDown.Load() {
		t.Error("shuttingDown should be true after Shutdown")
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

// --- SessionCache 測試 ---

func TestSessionCachePutAndGetAndDelete(t *testing.T) {
	sc := newSessionCache()

	sc.Put("key1", []byte("data1"))
	if sc.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", sc.Len())
	}

	data, ok := sc.GetAndDelete("key1")
	if !ok {
		t.Fatal("GetAndDelete returned false")
	}
	if string(data) != "data1" {
		t.Errorf("data = %q, want %q", data, "data1")
	}

	// 第二次取應該不存在（已刪除）
	_, ok = sc.GetAndDelete("key1")
	if ok {
		t.Error("GetAndDelete should return false on second call (replay prevention)")
	}
}

func TestSessionCacheMaxSize(t *testing.T) {
	sc := &SessionCache{
		entries: make(map[string]sessionEntry),
		maxSize: 3,
		ttl:     time.Hour,
	}

	sc.Put("a", []byte("1"))
	sc.Put("b", []byte("2"))
	sc.Put("c", []byte("3"))
	sc.Put("d", []byte("4")) // 應淘汰最舊

	if sc.Len() > 3 {
		t.Errorf("Len() = %d, want <= 3 (should evict)", sc.Len())
	}
}

func TestSessionCacheTTLExpiry(t *testing.T) {
	sc := &SessionCache{
		entries: make(map[string]sessionEntry),
		maxSize: 100,
		ttl:     50 * time.Millisecond,
	}

	sc.Put("expired", []byte("data"))
	time.Sleep(100 * time.Millisecond)

	_, ok := sc.GetAndDelete("expired")
	if ok {
		t.Error("expired session should not be returned")
	}
}

func TestSessionCacheConcurrency(t *testing.T) {
	sc := newSessionCache()
	var wg sync.WaitGroup

	// 並行寫入
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key" + string(rune('0'+n%10))
			sc.Put(key, []byte("data"))
		}(i)
	}

	// 並行讀取
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key" + string(rune('0'+n%10))
			sc.GetAndDelete(key)
		}(i)
	}

	wg.Wait()
	// 無 race condition panic 即通過
}

// --- NotFound / MethodNotAllowed 測試 ---

func TestServerNotFound(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	called := false
	s.NotFound(func(c *hypcontext.Context) {
		called = true
		c.Status(404)
		c.Writer.WriteHeaderNow()
	})

	// 驗證不會 panic（之前是空殼）
	if called {
		t.Error("handler should not be called yet (only registered)")
	}
}

// --- loadCertificate 測試 ---

func TestLoadCertificateError(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 不存在的證書路徑
	s.config.Server.TLS.CertFile = "/nonexistent/cert.pem"
	s.config.Server.TLS.KeyFile = "/nonexistent/key.pem"

	_, err := s.loadCertificate()
	if err == nil {
		t.Error("loadCertificate should return error for missing cert files")
	}
}

// --- Shutdown atomic 測試 ---

func TestShutdownAtomic(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 初始狀態
	if s.shuttingDown.Load() {
		t.Error("shuttingDown should be false initially")
	}

	// 並行讀寫不應 race
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Health()
		}()
	}

	s.shuttingDown.Store(true)
	wg.Wait()

	if !s.shuttingDown.Load() {
		t.Error("shuttingDown should be true after Store(true)")
	}
}
