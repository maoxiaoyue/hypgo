package watcher

import (
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestBuildSummaryCreated(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/new.go": fsnotify.Create,
	}
	s := BuildSummary(events)
	if len(s.Created) != 1 || s.Created[0] != "app/new.go" {
		t.Errorf("Created = %v, want [app/new.go]", s.Created)
	}
	if s.Total() != 1 {
		t.Errorf("Total = %d, want 1", s.Total())
	}
}

func TestBuildSummaryModified(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/main.go": fsnotify.Write,
	}
	s := BuildSummary(events)
	if len(s.Modified) != 1 {
		t.Errorf("Modified count = %d, want 1", len(s.Modified))
	}
}

func TestBuildSummaryDeleted(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/old.go": fsnotify.Remove,
	}
	s := BuildSummary(events)
	if len(s.Deleted) != 1 {
		t.Errorf("Deleted count = %d, want 1", len(s.Deleted))
	}
}

func TestBuildSummaryRenamed(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/renamed.go": fsnotify.Rename,
	}
	s := BuildSummary(events)
	if len(s.Deleted) != 1 {
		t.Error("Rename should be treated as delete")
	}
}

func TestBuildSummaryMixed(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/new.go":     fsnotify.Create,
		"app/changed.go": fsnotify.Write,
		"app/old.go":     fsnotify.Remove,
	}
	s := BuildSummary(events)
	if s.Total() != 3 {
		t.Errorf("Total = %d, want 3", s.Total())
	}
}

func TestBuildSummaryEmpty(t *testing.T) {
	s := BuildSummary(map[string]fsnotify.Op{})
	if !s.IsEmpty() {
		t.Error("empty events should be empty summary")
	}
}

func TestSummaryString(t *testing.T) {
	events := map[string]fsnotify.Op{
		"app/new.go":     fsnotify.Create,
		"app/changed.go": fsnotify.Write,
	}
	s := BuildSummary(events)
	str := s.String()

	if !strings.Contains(str, "Created (1)") {
		t.Error("should contain Created count")
	}
	if !strings.Contains(str, "Modified (1)") {
		t.Error("should contain Modified count")
	}
	if !strings.Contains(str, "+ app/new.go") {
		t.Error("should show created file with + prefix")
	}
	if !strings.Contains(str, "~ app/changed.go") {
		t.Error("should show modified file with ~ prefix")
	}
}

func TestSummaryStringEmpty(t *testing.T) {
	s := BuildSummary(map[string]fsnotify.Op{})
	if s.String() != "No changes detected" {
		t.Error("empty summary should say 'No changes detected'")
	}
}

func TestSummaryJSON(t *testing.T) {
	events := map[string]fsnotify.Op{
		"a.go": fsnotify.Create,
		"b.go": fsnotify.Write,
	}
	s := BuildSummary(events)
	j := s.JSON()

	if j["total"].(int) != 2 {
		t.Errorf("total = %v, want 2", j["total"])
	}
	if _, ok := j["timestamp"]; !ok {
		t.Error("should have timestamp")
	}
}

func TestShouldIgnore(t *testing.T) {
	w := &Watcher{
		config: Config{
			IgnorePatterns: defaultIgnorePatterns,
		},
	}

	tests := []struct {
		path   string
		ignore bool
	}{
		{".git", true},
		{".env", true},
		{"app/main.go", false},
		{"app/controllers/user.go", false},
		{"node_modules", true},
		{"vendor", true},
		{"test.swp", true},
		{"config.yaml", false},
	}

	for _, tt := range tests {
		if got := w.shouldIgnore(tt.path); got != tt.ignore {
			t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.ignore)
		}
	}
}

func TestSummarySorted(t *testing.T) {
	events := map[string]fsnotify.Op{
		"z.go": fsnotify.Create,
		"a.go": fsnotify.Create,
		"m.go": fsnotify.Create,
	}
	s := BuildSummary(events)
	if s.Created[0] != "a.go" || s.Created[1] != "m.go" || s.Created[2] != "z.go" {
		t.Errorf("Created should be sorted, got %v", s.Created)
	}
}
