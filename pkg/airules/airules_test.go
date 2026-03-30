package airules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/manifest"
)

func testManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version:   "1.0",
		Framework: "HypGo",
<<<<<<< dev_20260328
		Routes: []manifest.RouteManifest{
			{Method: "GET", Path: "/health", Summary: "Health check"},
			{Method: "POST", Path: "/api/users", Summary: "Create user"},
=======
		Server: manifest.ServerInfo{
			Addr:     ":8080",
			Protocol: "http2",
			TLS:      true,
		},
		Routes: []manifest.RouteManifest{
			{Method: "GET", Path: "/health", Summary: "Health check"},
			{Method: "POST", Path: "/api/users", Summary: "Create user", InputType: "CreateUserReq", OutputType: "UserResp"},
>>>>>>> main
		},
	}
}

func TestAllTargets(t *testing.T) {
	targets := AllTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}
<<<<<<< dev_20260328
=======

	names := make(map[string]bool)
	for _, tgt := range targets {
		names[tgt.Name] = true
	}
	for _, expected := range []string{"agents", "gemini", "copilot", "cursor", "windsurf"} {
		if !names[expected] {
			t.Errorf("missing target: %s", expected)
		}
	}
>>>>>>> main
}

func TestFilterTargets(t *testing.T) {
	targets := AllTargets()
<<<<<<< dev_20260328
	if len(FilterTargets(targets, "")) != 5 {
		t.Error("empty filter should return all")
	}
	if len(FilterTargets(targets, "agents")) != 1 {
		t.Error("should filter to 1")
	}
	if len(FilterTargets(targets, "agents,gemini")) != 2 {
		t.Error("should filter to 2")
	}
	if len(FilterTargets(targets, "nonexistent")) != 0 {
		t.Error("should filter to 0")
=======

	// 空字串 → 全部
	filtered := FilterTargets(targets, "")
	if len(filtered) != 5 {
		t.Errorf("empty filter should return all, got %d", len(filtered))
	}

	// 單一
	filtered = FilterTargets(targets, "agents")
	if len(filtered) != 1 || filtered[0].Name != "agents" {
		t.Errorf("expected [agents], got %v", filtered)
	}

	// 多個
	filtered = FilterTargets(targets, "agents,gemini")
	if len(filtered) != 2 {
		t.Errorf("expected 2, got %d", len(filtered))
	}

	// 不存在
	filtered = FilterTargets(targets, "nonexistent")
	if len(filtered) != 0 {
		t.Errorf("expected 0, got %d", len(filtered))
>>>>>>> main
	}
}

func TestGenerateAgentsMD(t *testing.T) {
	content := generateAgentsMD(testManifest())
<<<<<<< dev_20260328
	for _, want := range []string{autoGenMarker, "HypGo", "Schema-first", "/api/users", "Create user"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
=======

	if !strings.Contains(content, autoGenMarker) {
		t.Error("missing auto-gen marker")
	}
	if !strings.Contains(content, "HypGo Framework Instructions") {
		t.Error("missing title")
	}
	if !strings.Contains(content, "Schema-first routes") {
		t.Error("missing schema-first convention")
	}
	if !strings.Contains(content, "Typed errors") {
		t.Error("missing typed errors convention")
	}
	if !strings.Contains(content, ".hyp/context.yaml") {
		t.Error("missing manifest reference")
	}
	// 動態路由
	if !strings.Contains(content, "/api/users") {
		t.Error("missing dynamic route info")
	}
	if !strings.Contains(content, "Create user") {
		t.Error("missing route summary")
>>>>>>> main
	}
}

func TestGenerateCursorMDC(t *testing.T) {
	content := generateCursorMDC(testManifest())
<<<<<<< dev_20260328
	for _, want := range []string{"---", "globs: \"**/*.go\"", "alwaysApply: true"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateWindsurfLimit(t *testing.T) {
	content := generateWindsurfMD(testManifest())
=======

	if !strings.Contains(content, "---\n") {
		t.Error("missing frontmatter delimiter")
	}
	if !strings.Contains(content, "globs: \"**/*.go\"") {
		t.Error("missing globs in frontmatter")
	}
	if !strings.Contains(content, "alwaysApply: true") {
		t.Error("missing alwaysApply in frontmatter")
	}
	if !strings.Contains(content, "description: HypGo framework") {
		t.Error("missing description in frontmatter")
	}
}

func TestGenerateWindsurfMD(t *testing.T) {
	content := generateWindsurfMD(testManifest())

	if !strings.Contains(content, autoGenMarker) {
		t.Error("missing auto-gen marker")
	}
	// Windsurf 限制 6,000 字元
>>>>>>> main
	if len(content) > 6000 {
		t.Errorf("windsurf content exceeds 6000 chars: %d", len(content))
	}
}

func TestGenerateNilManifest(t *testing.T) {
<<<<<<< dev_20260328
=======
	// nil manifest 不應 panic
>>>>>>> main
	content := generateAgentsMD(nil)
	if !strings.Contains(content, "HypGo") {
		t.Error("nil manifest should still generate core content")
	}
<<<<<<< dev_20260328
	if strings.Contains(content, "## Current Routes") {
		t.Error("nil manifest should not have routes")
=======
	// 不應有路由表
	if strings.Contains(content, "## Current Routes") {
		t.Error("nil manifest should not have routes section")
	}
}

func TestGenerateEmptyRoutes(t *testing.T) {
	m := &manifest.Manifest{Routes: []manifest.RouteManifest{}}
	content := generateAgentsMD(m)
	if strings.Contains(content, "## Current Routes") {
		t.Error("empty routes should not have routes section")
>>>>>>> main
	}
}

func TestCanOverwrite(t *testing.T) {
	dir := t.TempDir()
<<<<<<< dev_20260328
	if !canOverwrite(filepath.Join(dir, "x.md")) {
		t.Error("nonexistent should be overwritable")
	}
	auto := filepath.Join(dir, "auto.md")
	os.WriteFile(auto, []byte(autoGenMarker+"\n# Content"), 0644)
	if !canOverwrite(auto) {
		t.Error("auto-generated should be overwritable")
	}
	manual := filepath.Join(dir, "manual.md")
	os.WriteFile(manual, []byte("# Custom"), 0644)
	if canOverwrite(manual) {
		t.Error("manual should NOT be overwritable")
=======

	// 不存在 → 可以
	if !canOverwrite(filepath.Join(dir, "nonexistent.md")) {
		t.Error("nonexistent file should be overwritable")
	}

	// 有標記 → 可以
	autoPath := filepath.Join(dir, "auto.md")
	os.WriteFile(autoPath, []byte(autoGenMarker+"\n# Content"), 0644)
	if !canOverwrite(autoPath) {
		t.Error("auto-generated file should be overwritable")
	}

	// 無標記 → 不行
	manualPath := filepath.Join(dir, "manual.md")
	os.WriteFile(manualPath, []byte("# My custom rules"), 0644)
	if canOverwrite(manualPath) {
		t.Error("manually created file should NOT be overwritable")
>>>>>>> main
	}
}

func TestGenerateAll(t *testing.T) {
	dir := t.TempDir()
<<<<<<< dev_20260328
	results, err := GenerateAll(dir, AllTargets(), testManifest(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusCreated {
			t.Errorf("%s: %s (%s)", r.Path, r.Status, r.Message)
=======
	targets := AllTargets()
	m := testManifest()

	results, err := GenerateAll(dir, targets, m, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Status != StatusCreated {
			t.Errorf("%s: expected created, got %s (%s)", r.Path, r.Status, r.Message)
		}
		// 檔案應該存在
		data, err := os.ReadFile(r.Path)
		if err != nil {
			t.Errorf("file not created: %s", r.Path)
			continue
		}
		if !strings.Contains(string(data), autoGenMarker) {
			t.Errorf("%s: missing auto-gen marker", r.Path)
>>>>>>> main
		}
	}
}

func TestGenerateAllDryRun(t *testing.T) {
	dir := t.TempDir()
<<<<<<< dev_20260328
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), true)
	if results[0].Status != StatusDryRun || results[0].Content == "" {
		t.Error("dry-run should return content without writing")
	}
=======
	targets := FilterTargets(AllTargets(), "agents")
	m := testManifest()

	results, err := GenerateAll(dir, targets, m, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusDryRun {
		t.Errorf("expected dry-run, got %s", results[0].Status)
	}
	if results[0].Content == "" {
		t.Error("dry-run should have content")
	}

	// 檔案不應被建立
>>>>>>> main
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("dry-run should not create files")
	}
}

<<<<<<< dev_20260328
func TestGenerateAllSkipsManual(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Custom"), 0644)
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), false)
	if results[0].Status != StatusSkipped {
		t.Error("should skip manual file")
=======
func TestGenerateAllSkipsManualFiles(t *testing.T) {
	dir := t.TempDir()

	// 預先建立一個手動檔案
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My custom rules"), 0644)

	targets := FilterTargets(AllTargets(), "agents")
	results, _ := GenerateAll(dir, targets, testManifest(), false)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", results[0].Status)
	}

	// 內容不應被覆蓋
	data, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(string(data), autoGenMarker) {
		t.Error("manual file should not be overwritten")
>>>>>>> main
	}
}

func TestStatusString(t *testing.T) {
<<<<<<< dev_20260328
	if StatusCreated.String() != "created" {
		t.Error("wrong string")
	}
	if Status(99).String() != "unknown" {
		t.Error("wrong string")
=======
	tests := map[Status]string{
		StatusCreated: "created",
		StatusSkipped: "skipped",
		StatusDryRun:  "dry-run",
		StatusError:   "error",
		Status(99):    "unknown",
	}
	for s, expected := range tests {
		if s.String() != expected {
			t.Errorf("Status(%d).String() = %q, want %q", s, s.String(), expected)
		}
>>>>>>> main
	}
}
