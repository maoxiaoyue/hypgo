package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
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
