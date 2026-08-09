package hypgo

import (
	"os"
	"path/filepath"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

func TestNewReturnsUsableApp(t *testing.T) {
	app := New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
	if app.Router == nil {
		t.Error("embedded Router is nil")
	}
	if app.Logger() == nil {
		t.Error("Logger() is nil")
	}
	if app.Server() == nil {
		t.Error("Server() is nil")
	}
	// 預設值已套用（即使無設定檔）
	if app.Config() == nil || app.Config().Server.Addr == "" {
		t.Error("Config defaults not applied (Addr empty)")
	}
}

func TestNewRegistersRoutesOnServerRouter(t *testing.T) {
	app := New()
	app.GET("/ping", func(c *hypcontext.Context) { c.String(200, "pong") })

	// 路由應註冊在 server 實際服務的同一個 router 上
	found := false
	for _, r := range app.Server().Router().Routes() {
		if r.Method == "GET" && r.Path == "/ping" {
			found = true
		}
	}
	if !found {
		t.Error("route registered via app is not on the server's router")
	}
}

func TestNewWithConfigMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, err := NewWithConfig(missing); err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestNewWithConfigLoads(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	content := "server:\n  addr: \":9999\"\n  protocol: http2\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	app, err := NewWithConfig(p)
	if err != nil {
		t.Fatalf("NewWithConfig failed: %v", err)
	}
	if app.Config().Server.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", app.Config().Server.Addr)
	}
	// 未指定的欄位應由 ApplyDefaults 填入
	if app.Config().Server.MaxConcurrentStreams == 0 {
		t.Error("defaults not applied (MaxConcurrentStreams == 0)")
	}
}

func TestWithConfigPathFallsBackOnMissing(t *testing.T) {
	// New（非 NewWithConfig）對缺檔應回退預設值，不 panic、不報錯
	app := New(WithConfigPath(filepath.Join(t.TempDir(), "absent.yaml")))
	if app == nil || app.Config().Server.Addr != ":8080" {
		t.Errorf("expected default Addr :8080 on missing config, got %v", app.Config().Server.Addr)
	}
}

func TestWithLogger(t *testing.T) {
	l := logger.NewLogger()
	app := New(WithLogger(l))
	if app.Logger() != l {
		t.Error("WithLogger was not applied")
	}
}
