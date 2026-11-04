package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/impact"
)

// runImpactCmd 執行 hyp impact，回傳 stdout
func runImpactCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		resetFlags(rootCmd)
	}()
	rootCmd.SetArgs(append([]string{"impact"}, args...))
	err := rootCmd.Execute()
	return out.String(), err
}

func impactProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		"go.mod":          "module example.com/p\n\ngo 1.24\n",
		"core/core.go":    "package core\n\nfunc Hello() {}\n",
		"svc/svc.go":      "package svc\n\nimport \"example.com/p/core\"\n\nfunc Call() { core.Hello() }\n",
		"svc/svc_test.go": "package svc\n\nimport \"testing\"\n\nfunc TestCall(t *testing.T) {}\n",
		"app/app.go":      "package app\n\nimport _ \"example.com/p/svc\"\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestImpactReportsDependents 迴歸測試：hyp impact 要找得到依賴者。
// 歷史 bug：CLI 以 "." 當根目錄，走訪第一步就被當成隱藏目錄跳過，任何檔案都回報沒有依賴者
func TestImpactReportsDependents(t *testing.T) {
	t.Chdir(impactProject(t))
	out, err := runImpactCmd(t, "core/core.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"→ svc", "uses: Hello", "app (via svc)", "go test ./svc/"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// TestImpactFromSubdirectory 迴歸測試：在子目錄執行也以 go.mod 所在目錄為根。
// 歷史 bug：固定用工作目錄當根，套件路徑與依賴全部算錯
func TestImpactFromSubdirectory(t *testing.T) {
	root := impactProject(t)
	t.Chdir(filepath.Join(root, "core"))
	out, err := runImpactCmd(t, "core.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Package: core") || !strings.Contains(out, "→ svc") {
		t.Errorf("report:\n%s", out)
	}
}

// TestImpactFailsOnMissingFile 迴歸測試：打錯路徑要 exit 非零。
// 歷史 bug：回報 LOW 並 exit 0，等於告訴 AI 可以放心改
func TestImpactFailsOnMissingFile(t *testing.T) {
	t.Chdir(impactProject(t))
	if out, err := runImpactCmd(t, "core/nope.go"); err == nil {
		t.Errorf("missing file should fail, got:\n%s", out)
	}
}

func TestImpactJSON(t *testing.T) {
	t.Chdir(impactProject(t))
	out, err := runImpactCmd(t, "-f", "json", "core/core.go")
	if err != nil {
		t.Fatal(err)
	}
	var report impact.ImpactReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if report.Package != "core" || len(report.Transitive) != 1 || report.TestCommand != "go test ./svc/" {
		t.Errorf("report = %+v", report)
	}
	if _, err := runImpactCmd(t, "-f", "xml", "core/core.go"); err == nil {
		t.Error("unknown format should fail")
	}
}
