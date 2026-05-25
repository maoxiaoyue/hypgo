package annotation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitLogValid(t *testing.T) {
	input := "abc1234def5678\x1fAlice\x1f2026-05-01\x1ffeat: add Login handler\n" +
		"beef0011beef0011\x1fBob\x1f2026-04-20\x1ffix: nil check on token"

	commits := parseGitLog(input)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Hash != "abc1234def5678" {
		t.Errorf("hash: got %q", commits[0].Hash)
	}
	if commits[0].Author != "Alice" {
		t.Errorf("author: got %q", commits[0].Author)
	}
	if commits[0].Date != "2026-05-01" {
		t.Errorf("date: got %q", commits[0].Date)
	}
	if commits[0].Subject != "feat: add Login handler" {
		t.Errorf("subject: got %q", commits[0].Subject)
	}
	if commits[1].Author != "Bob" {
		t.Errorf("second commit author: got %q", commits[1].Author)
	}
}

func TestParseGitLogEmpty(t *testing.T) {
	commits := parseGitLog("")
	if len(commits) != 0 {
		t.Errorf("expected 0, got %d", len(commits))
	}
}

func TestParseGitLogMalformed(t *testing.T) {
	// Mix of valid lines and patch/diff noise
	input := "diff --git a/foo.go b/foo.go\n" +
		"abc1234567890\x1fCharlie\x1f2026-03-10\x1frefactor: split router\n" +
		"+ func newRoute() {}\n" +
		"- old line\n" +
		"@@ -1,5 +1,6 @@\n"

	commits := parseGitLog(input)
	if len(commits) != 1 {
		t.Fatalf("expected 1, got %d", len(commits))
	}
	if commits[0].Author != "Charlie" {
		t.Errorf("got %q", commits[0].Author)
	}
}

func TestParseGitLogShortHash(t *testing.T) {
	// hash shorter than 7 chars should be skipped
	input := "abc12\x1fDave\x1f2026-01-01\x1fshould be skipped"
	commits := parseGitLog(input)
	if len(commits) != 0 {
		t.Errorf("expected 0 (short hash), got %d", len(commits))
	}
}

func TestGitContextIsEmpty(t *testing.T) {
	var nilCtx *GitContext
	if !nilCtx.IsEmpty() {
		t.Error("nil should be empty")
	}
	if !(&GitContext{}).IsEmpty() {
		t.Error("empty struct should be empty")
	}
	nonempty := &GitContext{FileCommits: []GitCommit{{Hash: "abc1234567"}}}
	if nonempty.IsEmpty() {
		t.Error("non-empty should not be empty")
	}
}

func TestGatherGitContextNonGitDir(t *testing.T) {
	// Create a temp directory that is NOT a git repository
	dir := t.TempDir()
	dummy := filepath.Join(dir, "dummy.go")
	if err := os.WriteFile(dummy, []byte("package dummy\n"), 0644); err != nil {
		t.Fatal(err)
	}

	gc := GatherGitContext(dummy, "Foo", "func", 5)
	if gc != nil {
		t.Errorf("expected nil for non-git dir, got %+v", gc)
	}
}

func TestIsPlaceholderHeadline(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"Create 建立新資源...", true},
		{"Create ...", true},
		{"TODO: 說明此區塊為何存在", true},
		{"CreateUser 建立使用者帳號並寫入資料庫", false},
		{"Package hidb provides master-replica routing", false},
	}
	for _, c := range cases {
		got := isPlaceholderHeadline(c.input)
		if got != c.want {
			t.Errorf("isPlaceholderHeadline(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestNeedsGitEnrichment(t *testing.T) {
	// No @ai tags → no enrichment needed
	r1 := CheckResult{ExistingTypes: nil, DocHeadline: ""}
	if needsGitEnrichment(r1) {
		t.Error("no @ai tags: should not enrich")
	}

	// Has @ai tags + placeholder headline → needs enrichment
	r2 := CheckResult{
		ExistingTypes: []AnnotationType{Generated},
		DocHeadline:   "Create ...",
	}
	if !needsGitEnrichment(r2) {
		t.Error("placeholder headline with @ai tags: should enrich")
	}

	// Has @ai tags + real headline → no enrichment needed
	r3 := CheckResult{
		ExistingTypes: []AnnotationType{Generated},
		DocHeadline:   "CreateUser writes a new user record to the database",
	}
	if needsGitEnrichment(r3) {
		t.Error("real headline: should not enrich")
	}
}
