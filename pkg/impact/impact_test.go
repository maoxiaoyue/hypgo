package impact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 建立測試用專案結構
func setupTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// pkg/core/core.go
	writeFile(t, root, "pkg/core/core.go", `package core

func Hello() string { return "hello" }
`)

	// pkg/core/core_test.go
	writeFile(t, root, "pkg/core/core_test.go", `package core

import "testing"

func TestHello(t *testing.T) {}
func TestHello2(t *testing.T) {}
`)

	// pkg/api/api.go — imports core
	writeFile(t, root, "pkg/api/api.go", `package api

import "example.com/project/pkg/core"

func Handler() { core.Hello() }
`)

	// pkg/api/api_test.go
	writeFile(t, root, "pkg/api/api_test.go", `package api

import "testing"

func TestHandler(t *testing.T) {}
`)

	// pkg/util/util.go — no dependency on core
	writeFile(t, root, "pkg/util/util.go", `package util

func Format() string { return "" }
`)

	return root
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- BuildImportGraph ---

func TestBuildImportGraph(t *testing.T) {
	root := setupTestProject(t)
	graph, err := BuildImportGraph(root)
	if err != nil {
		t.Fatalf("BuildImportGraph failed: %v", err)
	}

	// pkg/api should import something related to core
	apiImports, ok := graph["pkg/api"]
	if !ok {
		t.Fatal("pkg/api should be in graph")
	}

	found := false
	for _, imp := range apiImports {
		if strings.Contains(imp, "core") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pkg/api should import core, got: %v", apiImports)
	}
}

func TestBuildImportGraphSkipsHidden(t *testing.T) {
	root := setupTestProject(t)

	// 建立隱藏目錄
	writeFile(t, root, ".hidden/hidden.go", `package hidden
import "fmt"
func H() { fmt.Println() }
`)

	graph, err := BuildImportGraph(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := graph[".hidden"]; ok {
		t.Error("hidden directories should be skipped")
	}
}

// --- FindDependents ---

func TestFindDependents(t *testing.T) {
	root := setupTestProject(t)
	graph, _ := BuildImportGraph(root)

	dependents := FindDependents(graph, "pkg/core")

	found := false
	for _, dep := range dependents {
		if dep == "pkg/api" {
			found = true
		}
	}
	if !found {
		t.Errorf("pkg/api should depend on pkg/core, got: %v", dependents)
	}
}

func TestFindDependentsNone(t *testing.T) {
	root := setupTestProject(t)
	graph, _ := BuildImportGraph(root)

	dependents := FindDependents(graph, "pkg/util")
	if len(dependents) != 0 {
		t.Errorf("pkg/util should have no dependents, got: %v", dependents)
	}
}

// --- countTestsInPackage ---

func TestCountTestsInPackage(t *testing.T) {
	root := setupTestProject(t)

	count := countTestsInPackage(root, "pkg/core")
	if count != 2 {
		t.Errorf("pkg/core should have 2 tests, got %d", count)
	}

	count = countTestsInPackage(root, "pkg/api")
	if count != 1 {
		t.Errorf("pkg/api should have 1 test, got %d", count)
	}
}

func TestCountTestsNonexistent(t *testing.T) {
	root := setupTestProject(t)
	count := countTestsInPackage(root, "pkg/nonexistent")
	if count != 0 {
		t.Errorf("nonexistent package should have 0 tests, got %d", count)
	}
}

// --- Analyze ---

func TestAnalyze(t *testing.T) {
	root := setupTestProject(t)

	report, err := Analyze(filepath.Join(root, "pkg/core/core.go"), root)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.Package != "pkg/core" {
		t.Errorf("Package = %q, want %q", report.Package, "pkg/core")
	}

	// pkg/api depends on core
	foundDep := false
	for _, dep := range report.Dependents {
		if dep.Path == "pkg/api" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Error("pkg/api should be in dependents")
	}

	// 測試影響
	if len(report.Tests) == 0 {
		t.Error("should have affected tests")
	}

	if report.Risk == "" {
		t.Error("Risk should not be empty")
	}
}

func TestAnalyzeOutsideRoot(t *testing.T) {
	_, err := Analyze("/etc/passwd", "/home/user/project")
	if err == nil {
		t.Error("should reject files outside project root")
	}
}

// --- FormatReport ---

func TestFormatReport(t *testing.T) {
	report := &ImpactReport{
		Target:  "pkg/core/core.go",
		Package: "pkg/core",
		Dependents: []DependentFile{
			{Path: "pkg/api", Package: "api"},
		},
		Tests: []TestImpact{
			{File: "pkg/core/*_test.go", TestCount: 5},
		},
		Risk: "MEDIUM",
	}

	output := FormatReport(report)
	if !strings.Contains(output, "Impact Analysis") {
		t.Error("should contain header")
	}
	if !strings.Contains(output, "pkg/api") {
		t.Error("should contain dependent")
	}
	if !strings.Contains(output, "MEDIUM") {
		t.Error("should contain risk level")
	}
}

func TestFormatReportNoDependents(t *testing.T) {
	report := &ImpactReport{
		Target:  "pkg/util/util.go",
		Package: "pkg/util",
		Risk:    "LOW",
	}

	output := FormatReport(report)
	if !strings.Contains(output, "none") {
		t.Error("should indicate no dependents")
	}
}

// --- calculateRisk ---

func TestCalculateRisk(t *testing.T) {
	tests := []struct {
		deps  int
		tests int
		want  string
	}{
		{0, 0, "LOW"},
		{1, 10, "LOW"},
		{2, 15, "MEDIUM"},
		{3, 25, "MEDIUM"},
		{5, 10, "HIGH"},
		{1, 50, "HIGH"},
	}

	for _, tt := range tests {
		deps := make([]string, tt.deps)
		impacts := []TestImpact{{TestCount: tt.tests}}
		got := calculateRisk(deps, impacts)
		if got != tt.want {
			t.Errorf("calculateRisk(deps=%d, tests=%d) = %q, want %q",
				tt.deps, tt.tests, got, tt.want)
		}
	}
}

// --- packageFromFile ---

func TestPackageFromFile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"pkg/errors/catalog.go", "pkg/errors"},
		{"pkg/router/router.go", "pkg/router"},
		{"main.go", "."},
	}

	for _, tt := range tests {
		got := packageFromFile(tt.input)
		if got != tt.want {
			t.Errorf("packageFromFile(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
