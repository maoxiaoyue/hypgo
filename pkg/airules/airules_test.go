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
		Server: manifest.ServerInfo{
			Addr:     ":8080",
			Protocol: "http2",
			TLS:      true,
		},
		Routes: []manifest.RouteManifest{
			{Method: "GET", Path: "/health", Summary: "Health check"},
			{Method: "POST", Path: "/api/users", Summary: "Create user", InputType: "CreateUserReq", OutputType: "UserResp"},
		},
	}
}

func TestAllTargets(t *testing.T) {
	targets := AllTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}

	names := make(map[string]bool)
	for _, tgt := range targets {
		names[tgt.Name] = true
	}
	for _, expected := range []string{"agents", "gemini", "copilot", "cursor", "windsurf"} {
		if !names[expected] {
			t.Errorf("missing target: %s", expected)
		}
	}
}

func TestFilterTargets(t *testing.T) {
	targets := AllTargets()

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
	}
}

func TestGenerateAgentsMD(t *testing.T) {
	content := generateAgentsMD(testManifest())

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
	}
}

func TestGenerateCursorMDC(t *testing.T) {
	content := generateCursorMDC(testManifest())

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
	if len(content) > 6000 {
		t.Errorf("windsurf content exceeds 6000 chars: %d", len(content))
	}
}

func TestGenerateNilManifest(t *testing.T) {
	// nil manifest 不應 panic
	content := generateAgentsMD(nil)
	if !strings.Contains(content, "HypGo") {
		t.Error("nil manifest should still generate core content")
	}
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
	}
}

func TestCanOverwrite(t *testing.T) {
	dir := t.TempDir()

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
	}
}

func TestGenerateAll(t *testing.T) {
	dir := t.TempDir()
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
		}
	}
}

func TestGenerateAllDryRun(t *testing.T) {
	dir := t.TempDir()
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
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("dry-run should not create files")
	}
}

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
	}
}

func TestStatusString(t *testing.T) {
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
	}
}
