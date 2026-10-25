package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateModelRegistersWithMigrate 迴歸測試（CLI A5）：生成的 model 於 init() 註冊到 migrate.GlobalRegistry。
// 歷史 bug：model 從未註冊，hyp migrate diff / snapshot 永遠回報「No models registered」
func TestGenerateModelRegistersWithMigrate(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateModel(dir, "order"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "order.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{`"github.com/maoxiaoyue/hypgo/pkg/migrate"`, "migrate.GlobalRegistry().Register((*Order)(nil))"} {
		if !strings.Contains(src, want) {
			t.Errorf("generated model missing %s", want)
		}
	}
}
