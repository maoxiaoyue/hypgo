package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// 生成的設定檔在嚴格模式下要載入得起來：模板裡不能有 pkg/config 不認得的鍵
func TestGeneratedConfigsLoadInStrictMode(t *testing.T) {
	offlineGo(t)
	t.Setenv(config.StrictEnv, "1")
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"new", "web1"}, {"api", "api1"}, {"new", "cli", "cli1"}, {"new", "grpc", "grpc1"}, {"new", "desktop", "desk1"},
	} {
		if err := runHyp(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		name := args[len(args)-1]
		if _, err := config.LoadConfig(filepath.Join(name, "config", "config.yaml")); err != nil {
			t.Errorf("%s: generated config.yaml has warnings or errors: %v", name, err)
		}
		if _, err := config.LoadCommentConfig(filepath.Join(name, ".hyp", "comment.yaml")); err != nil {
			t.Errorf("%s: comment.yaml: %v", name, err)
		}
		if _, err := config.LoadLLMConfig(filepath.Join(name, ".hyp", "llm.yaml")); err != nil {
			t.Errorf("%s: llm.yaml: %v", name, err)
		}
		if _, err := config.LoadHypConfig(filepath.Join(name, ".hyp", "config.yaml")); err != nil {
			t.Errorf("%s: .hyp/config.yaml: %v", name, err)
		}
	}
}

// 設定檔的警告寫進 manifest 的 lint 區段，hyp lint 因此失敗。
// 歷史 bug：tls 底下寫 enable（少一個 d）沒有任何地方會提醒
func TestConfigWarningsReachManifestAndLint(t *testing.T) {
	var sink bytes.Buffer
	old := config.WarningOutput
	config.WarningOutput = &sink
	defer func() { config.WarningOutput = old }()

	dir := t.TempDir()
	t.Chdir(dir)
	os.WriteFile("config.yaml", []byte("server:\n  addr: :9443\n  tls:\n    enable: true\n"), 0o644)
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	m := manifest.NewCollector(router.New(), cfg).Collect()
	if m.Lint == nil || len(m.Lint.Warnings) != 1 || m.Lint.Warnings[0].Kind != "config_warning" ||
		!strings.Contains(m.Lint.Warnings[0].Message, "server.tls.enable") {
		t.Fatalf("lint = %+v", m.Lint)
	}

	os.MkdirAll(".hyp", 0o755)
	if err := manifest.SaveToFile(filepath.Join(".hyp", "context.yaml"), m, "yaml"); err != nil {
		t.Fatal(err)
	}
	if err := runHyp(t, "lint"); err == nil {
		t.Error("hyp lint should fail when the config has warnings")
	}

	// 乾淨的設定不影響 lint
	os.WriteFile("config.yaml", []byte("server:\n  addr: :9443\n"), 0o644)
	cfg, _ = config.LoadConfig("config.yaml")
	m = manifest.NewCollector(router.New(), cfg).Collect()
	manifest.SaveToFile(filepath.Join(".hyp", "context.yaml"), m, "yaml")
	if err := runHyp(t, "lint"); err != nil {
		t.Errorf("a clean config should not fail lint: %v", err)
	}
}
