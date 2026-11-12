package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 既有的 go.mod 是使用者的：只補缺少的 require，module 路徑、go 版本與既有版本都不動
func TestMergeGoMod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go.mod")
	existing := "module github.com/acme/tool\n\ngo 1.25.3\n\nrequire github.com/spf13/cobra v1.8.0\n"
	os.WriteFile(path, []byte(existing), 0o644)
	rendered := []byte("module tool\n\ngo 1.24\n\nrequire (\n\tgithub.com/maoxiaoyue/hypgo v0.9.3\n\tgithub.com/spf13/cobra v1.9.1\n)\n")

	changed, err := MergeGoMod(path, rendered)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"module github.com/acme/tool\n", "go 1.25.3\n", "github.com/spf13/cobra v1.8.0", "github.com/maoxiaoyue/hypgo v0.9.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "v1.9.1") {
		t.Errorf("an existing requirement was replaced:\n%s", got)
	}

	// 沒有缺的 require：不重寫檔案
	if changed, err := MergeGoMod(path, rendered); err != nil || changed {
		t.Errorf("second merge: changed=%v err=%v", changed, err)
	}

	// 既有 go.mod 不合法：回錯，不覆蓋
	os.WriteFile(path, []byte("not a go.mod"), 0o644)
	if _, err := MergeGoMod(path, rendered); err == nil {
		t.Error("an invalid go.mod should be an error")
	}
	if data, _ := os.ReadFile(path); string(data) != "not a go.mod" {
		t.Error("an invalid go.mod was overwritten")
	}
}

// KeepExisting 遇到既有 go.mod：合併 require 並回報，而不是單純略過
func TestKeepExistingMergesGoMod(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.24\n"), 0o644)

	var merged, kept []string
	KeepExisting = true
	OnMerge = func(p string) { merged = append(merged, filepath.Base(p)) }
	OnKeep = func(p string) { kept = append(kept, filepath.Base(p)) }
	defer func() { KeepExisting, OnMerge, OnKeep = false, nil, nil }()

	if err := GenerateCLIProject(dir, "demo", "example.com/demo"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.HasPrefix(string(data), "module example.com/demo\n") || !strings.Contains(string(data), "github.com/spf13/cobra") {
		t.Errorf("go.mod:\n%s", data)
	}
	if len(merged) != 1 || merged[0] != "go.mod" || len(kept) != 0 {
		t.Errorf("merged=%v kept=%v", merged, kept)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), `"example.com/demo/app/commands"`) {
		t.Error("imports should use the module path")
	}

	// 第二次：go.mod 已齊全，算保留
	merged, kept = nil, nil
	if err := GenerateCLIProject(dir, "demo", "example.com/demo"); err != nil {
		t.Fatal(err)
	}
	if len(merged) != 0 || !strings.Contains(strings.Join(kept, " "), "go.mod") {
		t.Errorf("second run: merged=%v kept=%v", merged, kept)
	}
}
