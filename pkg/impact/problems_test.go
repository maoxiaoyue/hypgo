package impact

import (
	"go/parser"
	"go/token"
	"testing"
)

// 舊式的 // +build ignore 也算不參與建置。歷史 bug：只認 //go:build，這種檔案被當成依賴者
func TestOldStyleBuildIgnore(t *testing.T) {
	for src, want := range map[string]bool{
		"// +build ignore\n\npackage main\n":            true,
		"//go:build ignore\n\npackage main\n":           true,
		"// +build linux\n\npackage main\n":             false,
		"// just a comment\n\npackage main\n":           false,
		"package main\n\n// +build ignore\nvar x = 1\n": false, // package 之後的不是 build 條件
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if got := buildIgnored(f); got != want {
			t.Errorf("%q: buildIgnored = %v, want %v", src, got, want)
		}
	}
}
