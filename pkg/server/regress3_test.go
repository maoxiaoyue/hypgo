package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/secret"
)

// TestOnShutdownHooks：Shutdown 執行 OnShutdown 登錄的函式並等它們返回。
// 先前沒有這個掛勾：升級成 WebSocket 的連線脫離了 HTTP server，Shutdown 之後仍然開著
func TestOnShutdownHooks(t *testing.T) {
	s := New(&config.Config{}, logger.NewLogger())
	var done atomic.Int32
	s.OnShutdown(nil)
	s.OnShutdown(func() { panic("hook panic must not break Shutdown") })
	s.OnShutdown(func() {
		time.Sleep(150 * time.Millisecond)
		done.Add(1)
	})
	s.OnShutdown(func() { done.Add(1) })
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := done.Load(); n != 2 {
		t.Errorf("Shutdown returned with %d of 2 hooks finished", n)
	}

	// 不返回的函式不會讓 Shutdown 超過期限
	s = New(&config.Config{}, logger.NewLogger())
	block := make(chan struct{})
	defer close(block)
	s.OnShutdown(func() { <-block })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	s.Shutdown(ctx)
	if d := time.Since(start); d < 150*time.Millisecond || d > 2*time.Second {
		t.Errorf("Shutdown with a hanging hook took %v, want about 200ms", d)
	}
}

// 先前 HYPGO_SECRET_OPTIONAL 留在執行環境時服務照樣啟動，資料庫密碼是字面的 "${secret:db_pass}"。
// hyp context / hyp migrate 的執行方式（寫出 manifest 後返回）不受影響
func TestStartRefusesOptionalSecretEnv(t *testing.T) {
	t.Setenv(secret.OptionalEnv, "1")
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"

	errCh := make(chan error, 1)
	s := New(&cfg, logger.NewLogger())
	go func() { errCh <- s.Start() }()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), secret.OptionalEnv) {
			t.Fatalf("Start() = %v", err)
		}
	case <-time.After(5 * time.Second):
		s.Shutdown(context.Background())
		t.Fatal("server started with " + secret.OptionalEnv + " set")
	}

	out := filepath.Join(t.TempDir(), "manifest.json")
	t.Setenv(manifest.ManifestOutEnv, out)
	if err := New(&cfg, logger.NewLogger()).Start(); err != nil {
		t.Fatalf("manifest mode: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
}
