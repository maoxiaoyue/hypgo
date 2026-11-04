package hypgo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestNewPanicsOnInvalidConfig 迴歸測試：設定檔存在但無效時 New 必須 panic。
// 歷史 bug：靜默退回內建預設值——:9443 + TLS 的正式設定只要有一個值不合法就跑成 :8080 明文，
// 缺機密金鑰時則跑成沒有資料庫設定，全程沒有任何訊息
func TestNewPanicsOnInvalidConfig(t *testing.T) {
	t.Setenv("HYPGO_MASTER_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for name, content := range map[string]string{
		"validation failure": "server:\n  addr: ':9443'\n  protocol: nonsense\n",
		"malformed yaml":     "server: [\n",
		"secret unavailable": "database:\n  dsn: \"${secret:db_pass}\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("New silently fell back to defaults instead of panicking")
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, path) {
					t.Errorf("panic should name the file: %s", msg)
				}
			}()
			New(WithConfigPath(path))
		})
	}
}

// TestNewProbeSkipsMissingFiles 沒有任何設定檔時維持零配置：不 panic，使用內建預設值
func TestNewProbeSkipsMissingFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if app := New(); app.Config().Server.Addr != ":8080" {
		t.Errorf("Addr = %q, want the default :8080", app.Config().Server.Addr)
	}
}

func TestWithLogger(t *testing.T) {
	l := logger.NewLogger()
	app := New(WithLogger(l))
	if app.Logger() != l {
		t.Error("WithLogger was not applied")
	}
}
