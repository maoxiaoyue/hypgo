package annotation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParseAnnotations ---

func TestParseAnnotationsBasic(t *testing.T) {
	input := `// Create 建立使用者
// @ai:constraint max_items=100
// @ai:security requires_auth`

	anns := ParseAnnotations(input)
	if len(anns) != 2 {
		t.Fatalf("got %d annotations, want 2", len(anns))
	}
	if anns[0].Type != Constraint || anns[0].Value != "max_items=100" {
		t.Errorf("anns[0] = %+v, want constraint max_items=100", anns[0])
	}
	if anns[1].Type != Security || anns[1].Value != "requires_auth" {
		t.Errorf("anns[1] = %+v, want security requires_auth", anns[1])
	}
}

func TestParseAnnotationsAllTypes(t *testing.T) {
	input := `// @ai:constraint limit=50
// @ai:deprecated use V2 instead
// @ai:security requires_admin
// @ai:impact routes=/api/users
// @ai:owner team=backend`

	anns := ParseAnnotations(input)
	if len(anns) != 5 {
		t.Fatalf("got %d annotations, want 5", len(anns))
	}

	expected := []AnnotationType{Constraint, Deprecated, Security, Impact, Owner}
	for i, want := range expected {
		if anns[i].Type != want {
			t.Errorf("anns[%d].Type = %q, want %q", i, anns[i].Type, want)
		}
	}
}

func TestParseAnnotationsNoAnnotations(t *testing.T) {
	input := `// This is a normal comment
// Another comment without @ai prefix`

	anns := ParseAnnotations(input)
	if len(anns) != 0 {
		t.Errorf("got %d annotations, want 0", len(anns))
	}
}

func TestParseAnnotationsEmpty(t *testing.T) {
	anns := ParseAnnotations("")
	if len(anns) != 0 {
		t.Errorf("got %d annotations, want 0", len(anns))
	}
}

func TestParseAnnotationsUnknownType(t *testing.T) {
	input := `// @ai:unknown something`
	anns := ParseAnnotations(input)
	if len(anns) != 0 {
		t.Errorf("unknown type should be ignored, got %d", len(anns))
	}
}

func TestParseAnnotationsNoValue(t *testing.T) {
	input := `// @ai:deprecated`
	anns := ParseAnnotations(input)
	if len(anns) != 1 {
		t.Fatalf("got %d, want 1", len(anns))
	}
	if anns[0].Value != "" {
		t.Errorf("Value = %q, want empty", anns[0].Value)
	}
}

// --- FormatAnnotation ---

func TestFormatAnnotation(t *testing.T) {
	tests := []struct {
		input Annotation
		want  string
	}{
		{Annotation{Type: Constraint, Value: "max=100"}, "// @ai:constraint max=100"},
		{Annotation{Type: Deprecated, Value: "use V2"}, "// @ai:deprecated use V2"},
		{Annotation{Type: Security, Value: ""}, "// @ai:security"},
	}
	for _, tt := range tests {
		got := FormatAnnotation(tt.input)
		if got != tt.want {
			t.Errorf("FormatAnnotation(%+v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- FormatAnnotation → ParseAnnotations 往返 ---

func TestAnnotationRoundTrip(t *testing.T) {
	original := Annotation{Type: Impact, Value: "routes=/api/users, models=User"}
	formatted := FormatAnnotation(original)
	parsed := ParseAnnotations(formatted)

	if len(parsed) != 1 {
		t.Fatalf("round trip: got %d annotations, want 1", len(parsed))
	}
	if parsed[0].Type != original.Type || parsed[0].Value != original.Value {
		t.Errorf("round trip failed: got %+v, want %+v", parsed[0], original)
	}
}

// --- CheckFile ---

func TestCheckFileValid(t *testing.T) {
	// 建立暫存 Go 檔案
	content := `// Package testpkg is a test package
package testpkg

// ExportedFunc does something
func ExportedFunc() {}

func unexportedFunc() {}

// ExportedType is a type
type ExportedType struct{}

type unexportedType struct{}

// ExportedConst is a constant
const ExportedConst = 42
`
	tmpFile := writeTempGoFile(t, content)

	report, err := CheckFile(tmpFile)
	if err != nil {
		t.Fatalf("CheckFile failed: %v", err)
	}

	// package + ExportedFunc + ExportedType + ExportedConst = 4 exported blocks
	if report.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Total)
	}
	if report.Passed != 4 {
		t.Errorf("Passed = %d, want 4 (all have comments)", report.Passed)
	}
}

func TestCheckFileMissingComments(t *testing.T) {
	content := `package testpkg

func ExportedFunc() {}

type ExportedType struct{}
`
	tmpFile := writeTempGoFile(t, content)

	report, err := CheckFile(tmpFile)
	if err != nil {
		t.Fatalf("CheckFile failed: %v", err)
	}

	// package (no doc) + ExportedFunc (no doc) + ExportedType (no doc) = 3
	if report.Total != 3 {
		t.Errorf("Total = %d, want 3", report.Total)
	}
	// package has no doc comment
	if report.Passed != 0 {
		t.Errorf("Passed = %d, want 0", report.Passed)
	}
}

func TestCheckFileRejectsNonGo(t *testing.T) {
	_, err := CheckFile("test.txt")
	if err == nil {
		t.Error("should reject non-.go files")
	}
}

func TestCheckFileMethod(t *testing.T) {
	content := `package testpkg

// Server is a test server
type Server struct{}

// Start starts the server
func (s *Server) Start() {}

func (s *Server) Stop() {}
`
	tmpFile := writeTempGoFile(t, content)

	report, err := CheckFile(tmpFile)
	if err != nil {
		t.Fatalf("CheckFile failed: %v", err)
	}

	// package (no doc) + Server (has doc) + Start (has doc) + Stop (no doc) = 4
	var stopResult *CheckResult
	for i, r := range report.Results {
		if strings.Contains(r.Name, "Stop") {
			stopResult = &report.Results[i]
		}
	}
	if stopResult == nil {
		t.Fatal("Stop method not found in results")
	}
	if stopResult.HasDoc {
		t.Error("Stop should not have doc comment")
	}
	if stopResult.Kind != "method" {
		t.Errorf("Stop.Kind = %q, want %q", stopResult.Kind, "method")
	}
}

// --- FormatReport ---

func TestFormatReport(t *testing.T) {
	report := &CheckReport{
		Filename: "test.go",
		Total:    3,
		Passed:   1,
		Results: []CheckResult{
			{Name: "package test", Kind: "package", HasDoc: true},
			{Name: "func Create", Kind: "func", HasDoc: false, Suggested: "// Create ..."},
			{Name: "type User", Kind: "type", HasDoc: false, Suggested: "// User ..."},
		},
	}

	output := FormatReport(report)
	if !strings.Contains(output, "[PASS]") {
		t.Error("should contain [PASS]")
	}
	if !strings.Contains(output, "[FAIL]") {
		t.Error("should contain [FAIL]")
	}
	if !strings.Contains(output, "1/3") {
		t.Error("should contain 1/3 summary")
	}
	if !strings.Contains(output, "--fix") {
		t.Error("should suggest --fix")
	}
}

// --- FixFile ---

func TestFixFile(t *testing.T) {
	content := `package testpkg

func ExportedFunc() {}
`
	tmpFile := writeTempGoFile(t, content)

	report, err := CheckFile(tmpFile)
	if err != nil {
		t.Fatalf("CheckFile failed: %v", err)
	}

	if err := FixFile(tmpFile, report.Results); err != nil {
		t.Fatalf("FixFile failed: %v", err)
	}

	// 確認備份存在
	if _, err := os.Stat(tmpFile + ".bak"); os.IsNotExist(err) {
		t.Error("backup file should be created")
	}

	// 確認註解已加入
	fixed, _ := os.ReadFile(tmpFile)
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "// Package testpkg") {
		t.Error("package comment should be added")
	}
	if !strings.Contains(fixedStr, "// ExportedFunc") {
		t.Error("func comment should be added")
	}
}

// --- ValidAnnotationTypes ---

func TestValidAnnotationTypes(t *testing.T) {
	types := ValidAnnotationTypes()
	if len(types) != 5 {
		t.Errorf("got %d types, want 5", len(types))
	}
}

// --- helpers ---

func writeTempGoFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
