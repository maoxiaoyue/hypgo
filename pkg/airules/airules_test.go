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

func TestAllTargets(t *testing.T) {
	targets := AllTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}
}

func TestFilterTargets(t *testing.T) {
	targets := AllTargets()
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
	}
}

func TestGenerateAgentsMD(t *testing.T) {
	content := generateAgentsMD(testManifest())
	for _, want := range []string{autoGenMarker, "HypGo", "Schema-first", "/api/users", "Create user"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateCursorMDC(t *testing.T) {
	content := generateCursorMDC(testManifest())
	for _, want := range []string{"---", "globs: \"**/*.go\"", "alwaysApply: true"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateWindsurfLimit(t *testing.T) {
	content := generateWindsurfMD(testManifest())
	if len(content) > 6000 {
		t.Errorf("windsurf content exceeds 6000 chars: %d", len(content))
	}
}

func TestGenerateNilManifest(t *testing.T) {
	content := generateAgentsMD(nil)
	if !strings.Contains(content, "HypGo") {
		t.Error("nil manifest should still generate core content")
	}
	if strings.Contains(content, "## Current Routes") {
		t.Error("nil manifest should not have routes")
	}
}

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
	results, err := GenerateAll(dir, AllTargets(), testManifest(), false)
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
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), true)
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
	results, _ := GenerateAll(dir, FilterTargets(AllTargets(), "agents"), testManifest(), false)
	if results[0].Status != StatusSkipped {
		t.Error("should skip manual file")
	}
}

func TestStatusString(t *testing.T) {
	if StatusCreated.String() != "created" {
		t.Error("wrong string")
	}
	if Status(99).String() != "unknown" {
		t.Error("wrong string")
	}
}
