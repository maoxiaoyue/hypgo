package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

func setupAutoSyncRouter() *router.Router {
	schema.Global().Reset()
	r := router.New()
	r.GET("/health", func(c *hypcontext.Context) {})
	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/users",
		Summary: "Create user",
	}).Handle(func(c *hypcontext.Context) {})
	return r
}

func TestAutoSyncCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".hyp", "context.yaml")

	r := setupAutoSyncRouter()
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path}, r, nil, nil)

	if err := a.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	content, _ := os.ReadFile(path)
	if len(content) == 0 {
		t.Fatal("file should not be empty")
	}
}

func TestAutoSyncDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.yaml")

	r := router.New()
	a := NewAutoSync(AutoSyncConfig{Enabled: false, Path: path}, r, nil, nil)

	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("file should not exist when disabled")
	}
}

func TestAutoSyncWithConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.yaml")

	r := setupAutoSyncRouter()
	cfg := &config.Config{}
	cfg.Server.Addr = ":8080"
	cfg.Server.Protocol = "http2"

	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path}, r, cfg, nil)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(path)
	if len(content) == 0 {
		t.Error("should have content")
	}
}

func TestAutoSyncJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.json")

	r := setupAutoSyncRouter()
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, Format: "json"}, r, nil, nil)

	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(path)
	if len(content) == 0 || content[0] != '{' {
		t.Error("JSON output should start with {")
	}
}

func TestAutoSyncSafe(t *testing.T) {
	r := router.New()
	schema.Global().Reset()
	// 使用不可寫的路徑，不應 panic
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: "/nonexistent/deep/path/ctx.yaml"}, r, nil, nil)
	a.SyncSafe() // 不應 panic
}

func TestAutoSyncDefaultPath(t *testing.T) {
	// 預設寫到 module 根目錄的 .hyp（往上找 go.mod），不是工作目錄。
	// 歷史 bug：從子目錄啟動或 go test 時寫到該目錄，hyp lint 讀的卻是根目錄那一份
	root := t.TempDir()
	sub := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	a := NewAutoSync(AutoSyncConfig{Enabled: true}, router.New(), nil, nil)
	want, _ := filepath.EvalSymlinks(root)
	got, _ := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(a.config.Path)))
	if got != want || filepath.Base(a.config.Path) != "context.yaml" {
		t.Errorf("default path = %q, want %s/.hyp/context.yaml", a.config.Path, root)
	}
}

func TestAutoSyncDefaultFormat(t *testing.T) {
	a := NewAutoSync(AutoSyncConfig{Enabled: true}, router.New(), nil, nil)
	if a.config.Format != "yaml" {
		t.Errorf("default format = %q, want yaml", a.config.Format)
	}
}
