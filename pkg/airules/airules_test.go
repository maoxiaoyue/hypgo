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
		Routes: []manifest.RouteManifest{
			{Method: "GET", Path: "/health", Summary: "Health check"},
			{Method: "POST", Path: "/api/users", Summary: "Create user"},
		},
	}
}

var defaultOpts = Options{}
var diffLogOpts = Options{DiffLogEnabled: true}

const (
	expectedAllTargets     = 15
	expectedDefaultTargets = 7
)

func TestAllTargets(t *testing.T) {
	targets := AllTargets()
	if len(targets) != expectedAllTargets {
		t.Fatalf("expected %d targets, got %d", expectedAllTargets, len(targets))
	}
	// 確保名稱唯一
	seen := make(map[string]bool)
	for _, tt := range targets {
		if seen[tt.Name] {
			t.Errorf("duplicate target name: %s", tt.Name)
		}
		seen[tt.Name] = true
	}
}

func TestDefaultTargets(t *testing.T) {
	targets := DefaultTargets()
	if len(targets) != expectedDefaultTargets {
		t.Fatalf("expected %d default targets, got %d", expectedDefaultTargets, len(targets))
	}
	wantNames := []string{"chatgpt", "claude", "trae", "tongyi", "deepseek", "cursor", "gemini"}
	for i, want := range wantNames {
		if targets[i].Name != want {
			t.Errorf("default[%d] = %s, want %s", i, targets[i].Name, want)
		}
	}
}

func TestResolveTargets(t *testing.T) {
	cases := []struct {
		only string
		want int
	}{
		{"", expectedDefaultTargets},
		{"all", expectedDefaultTargets},
		{"ALL", expectedDefaultTargets},
		{"  all  ", expectedDefaultTargets},
		{"every", expectedAllTargets},
		{"*", expectedAllTargets},
		{"agents", 1},
		{"agents,copilot", 2},
		{"chatgpt,claude,trae", 3},
		{"nonexistent", 0},
	}
	for _, c := range cases {
		got := ResolveTargets(c.only)
		if len(got) != c.want {
			t.Errorf("ResolveTargets(%q): got %d, want %d", c.only, len(got), c.want)
		}
	}
}

func TestFilterTargets(t *testing.T) {
	targets := AllTargets()
	if len(FilterTargets(targets, "")) != len(targets) {
		t.Error("empty filter should return all (pure-function semantics)")
	}
	if len(FilterTargets(targets, "agents")) != 1 {
		t.Error("should filter to 1")
	}
	if len(FilterTargets(targets, "agents,gemini")) != 2 {
		t.Error("should filter to 2")
	}
	if len(FilterTargets(targets, "AGENTS, Gemini ")) != 2 {
		t.Error("filter should be case-insensitive and trim spaces")
	}
	if len(FilterTargets(targets, "nonexistent")) != 0 {
		t.Error("should filter to 0")
	}
}

func TestGenerateAgentsMD(t *testing.T) {
	content := generateAgentsMD(testManifest(), defaultOpts)
	for _, want := range []string{autoGenMarker, "HypGo", "Schema-first", "/api/users", "Create user"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateClaudeMD(t *testing.T) {
	content := generateClaudeMD(testManifest(), defaultOpts)
	for _, want := range []string{autoGenMarker, "Claude Code Instructions", "HypGo", "Schema-first"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateChatGPTMD(t *testing.T) {
	content := generateChatGPTMD(testManifest(), defaultOpts)
	if !strings.Contains(content, "ChatGPT Instructions") {
		t.Error("missing ChatGPT title")
	}
}

func TestGenerateTraeMD(t *testing.T) {
	content := generateTraeMD(testManifest(), defaultOpts)
	if !strings.Contains(content, "Trae Project Rules") {
		t.Error("missing Trae title")
	}
}

func TestGenerateTongyiMD(t *testing.T) {
	content := generateTongyiMD(testManifest(), defaultOpts)
	if !strings.Contains(content, "通義靈碼") {
		t.Error("missing Tongyi (通義靈碼) title")
	}
}

func TestGenerateDeepSeekMD(t *testing.T) {
	content := generateDeepSeekMD(testManifest(), defaultOpts)
	if !strings.Contains(content, "DeepSeek Instructions") {
		t.Error("missing DeepSeek title")
	}
}

func TestGenerateCursorMDC(t *testing.T) {
	content := generateCursorMDC(testManifest(), defaultOpts)
	for _, want := range []string{"---", "globs: \"**/*.go\"", "alwaysApply: true"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateWindsurfLimit(t *testing.T) {
	content := generateWindsurfMD(testManifest(), defaultOpts)
	if len(content) > 6000 {
		t.Errorf("windsurf content exceeds 6000 chars: %d", len(content))
	}
}

func TestGenerateNilManifest(t *testing.T) {
	content := generateAgentsMD(nil, defaultOpts)
	if !strings.Contains(content, "HypGo") {
		t.Error("nil manifest should still generate core content")
	}
	if strings.Contains(content, "## Current Routes") {
		t.Error("nil manifest should not have routes")
	}
}

// --- DiffLog 開關測試 ---

func TestDiffLogEnabled(t *testing.T) {
	content := generateAgentsMD(nil, diffLogOpts)
	if !strings.Contains(content, "hyp diff-log") {
		t.Error("diff-log ON: should include 'hyp diff-log' instruction")
	}
}

func TestDiffLogDisabled(t *testing.T) {
	content := generateAgentsMD(nil, defaultOpts)
	if strings.Contains(content, "hyp diff-log") {
		t.Error("diff-log OFF: should NOT include 'hyp diff-log' instruction")
	}
}

// --- 共用測試 ---

func TestCanOverwrite(t *testing.T) {
	dir := t.TempDir()
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
	}
}

func TestGenerateAll(t *testing.T) {
	dir := t.TempDir()
	results, err := GenerateAll(dir, AllTargets(), testManifest(), defaultOpts, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusCreated {
			t.Errorf("%s: %s (%s)", r.Path, r.Status, r.Message)
		}
	}
}

func TestGenerateAllDryRun(t *testing.T) {
	dir := t.TempDir()
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), defaultOpts, true)
	if results[0].Status != StatusDryRun || results[0].Content == "" {
		t.Error("dry-run should return content without writing")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Error("dry-run should not create files")
	}
}

func TestGenerateAllSkipsManual(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Custom"), 0644)
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), defaultOpts, false)
	if results[0].Status != StatusSkipped {
		t.Error("should skip manual file")
	}
}

func TestStatusString(t *testing.T) {
	if StatusCreated.String() != "created" {
		t.Error("wrong string")
	}
	if StatusRemoved.String() != "removed" {
		t.Error("StatusRemoved string mismatch")
	}
	if Status(99).String() != "unknown" {
		t.Error("wrong string")
	}
}

// --- Clean 測試 ---

func TestResolveCleanTargets(t *testing.T) {
	cases := []struct {
		only string
		want int
	}{
		{"", expectedAllTargets},
		{"all", expectedAllTargets},
		{"every", expectedAllTargets},
		{"*", expectedAllTargets},
		{"agents", 1},
		{"agents,copilot", 2},
		{"nonexistent", 0},
	}
	for _, c := range cases {
		got := ResolveCleanTargets(c.only)
		if len(got) != c.want {
			t.Errorf("ResolveCleanTargets(%q): got %d, want %d", c.only, len(got), c.want)
		}
	}
}

func TestCleanAllRemovesGenerated(t *testing.T) {
	dir := t.TempDir()
	if _, err := GenerateAll(dir, AllTargets(), testManifest(), defaultOpts, false); err != nil {
		t.Fatal(err)
	}
	results, err := CleanAll(dir, AllTargets(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusRemoved {
			t.Errorf("%s: status=%s (%s)", r.Path, r.Status, r.Message)
		}
	}
	// 全部目標檔案都應消失
	for _, target := range AllTargets() {
		path := filepath.Join(dir, target.RelPath)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", path)
		}
	}
}

func TestCleanAllRemovesEmptyParents(t *testing.T) {
	dir := t.TempDir()
	// 生成 cursor（會建立 .cursor/rules/ 目錄）
	GenerateAll(dir, FilterTargets(AllTargets(), "cursor"), testManifest(), defaultOpts, false)
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "rules")); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	CleanAll(dir, FilterTargets(AllTargets(), "cursor"), false)
	// .cursor/rules/ 與 .cursor/ 都應因為變空而被移除
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "rules")); !os.IsNotExist(err) {
		t.Error(".cursor/rules/ should be removed (empty)")
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); !os.IsNotExist(err) {
		t.Error(".cursor/ should be removed (empty)")
	}
	// 專案根本身絕對保留
	if _, err := os.Stat(dir); err != nil {
		t.Error("dir (project root) must never be removed")
	}
}

func TestCleanAllPreservesNonEmptyParents(t *testing.T) {
	dir := t.TempDir()
	// 在 .cursor/ 下放一個非生成的檔案
	cursorDir := filepath.Join(dir, ".cursor")
	os.MkdirAll(cursorDir, 0755)
	keepPath := filepath.Join(cursorDir, "user_notes.md")
	os.WriteFile(keepPath, []byte("# My notes"), 0644)
	// 生成 cursor 規則檔
	GenerateAll(dir, FilterTargets(AllTargets(), "cursor"), testManifest(), defaultOpts, false)
	// Clean
	CleanAll(dir, FilterTargets(AllTargets(), "cursor"), false)
	// .cursor/ 不應被移除（user_notes.md 還在）
	if _, err := os.Stat(keepPath); err != nil {
		t.Error("user file should be preserved")
	}
	if _, err := os.Stat(cursorDir); err != nil {
		t.Error(".cursor/ should remain (non-empty)")
	}
}

func TestCleanAllSkipsManualFiles(t *testing.T) {
	dir := t.TempDir()
	manualPath := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(manualPath, []byte("# My custom rules — not auto-gen"), 0644)
	results, _ := CleanAll(dir, FilterTargets(AllTargets(), "agents"), false)
	if results[0].Status != StatusSkipped {
		t.Errorf("manual file should be skipped, got %s", results[0].Status)
	}
	if _, err := os.Stat(manualPath); err != nil {
		t.Error("manual file must not be removed")
	}
}

func TestCleanAllSkipsNonexistent(t *testing.T) {
	dir := t.TempDir()
	results, _ := CleanAll(dir, FilterTargets(AllTargets(), "agents"), false)
	if results[0].Status != StatusSkipped || results[0].Message != "not found" {
		t.Errorf("nonexistent should be skipped/not found, got %s/%s", results[0].Status, results[0].Message)
	}
}

func TestCleanAllDryRun(t *testing.T) {
	dir := t.TempDir()
	GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), defaultOpts, false)
	results, _ := CleanAll(dir, FilterTargets(AllTargets(), "agents"), true)
	if results[0].Status != StatusDryRun {
		t.Errorf("expected dry-run, got %s", results[0].Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Error("dry-run should not remove file")
	}
}
