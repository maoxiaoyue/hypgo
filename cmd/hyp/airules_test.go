package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// airulesProject 建立含 go.mod 的暫存專案並切過去
func airulesProject(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestAIRulesDefaultSet(t *testing.T) {
	root := airulesProject(t)
	if err := runHyp(t, "ai-rules"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		if !exists(filepath.Join(root, want)) {
			t.Errorf("%s not generated", want)
		}
	}
	for _, not := range []string{"CHATGPT.md", "DEEPSEEK.md"} {
		if exists(filepath.Join(root, not)) {
			t.Errorf("%s should not be in the default set", not)
		}
	}
}

// 打錯子命令要回錯：先前 hyp ai-rules claen 會執行生成
func TestAIRulesRejectsArguments(t *testing.T) {
	root := airulesProject(t)
	for _, args := range [][]string{{"ai-rules", "claen"}, {"ai-rules", "clean", "extra"}} {
		if err := runHyp(t, args...); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
	if exists(filepath.Join(root, "AGENTS.md")) {
		t.Error("a mistyped subcommand generated files")
	}
}

func TestAIRulesRejectsUnknownTargets(t *testing.T) {
	root := airulesProject(t)
	for _, only := range []string{"agents,cursro", "all,agents", "every,agents"} {
		if err := runHyp(t, "ai-rules", "--only", only); err == nil {
			t.Errorf("--only %s should fail", only)
		}
		if err := runHyp(t, "ai-rules", "clean", "--only", only); err == nil {
			t.Errorf("clean --only %s should fail", only)
		}
	}
	if exists(filepath.Join(root, "AGENTS.md")) {
		t.Error("nothing should be generated when --only is invalid")
	}
}

// --dir 打錯不能建立新目錄
func TestAIRulesDirMustExist(t *testing.T) {
	root := airulesProject(t)
	for _, args := range [][]string{{"ai-rules", "--dir", "typo"}, {"ai-rules", "clean", "--dir", "typo"}} {
		if err := runHyp(t, args...); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
	if exists(filepath.Join(root, "typo")) {
		t.Error("--dir created a directory")
	}

	// 指向檔案也不行；存在的目錄不需要 go.mod
	if err := runHyp(t, "ai-rules", "--dir", "go.mod"); err == nil {
		t.Error("--dir pointing at a file should fail")
	}
	other := t.TempDir()
	if err := runHyp(t, "ai-rules", "--dir", other, "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(other, "AGENTS.md")) {
		t.Error("explicit --dir ignored")
	}
}

// 在子目錄執行：寫到專案根目錄，也讀得到根目錄的 .hyp/comment.yaml
func TestAIRulesFindsModuleRoot(t *testing.T) {
	root := airulesProject(t)
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
	os.WriteFile(filepath.Join(root, ".hyp", "comment.yaml"), []byte("ai_comment: true\n"), 0o644)
	sub := filepath.Join(root, "routers")
	os.Mkdir(sub, 0o755)
	t.Chdir(sub)

	if err := runHyp(t, "ai-rules", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(sub, "AGENTS.md")) {
		t.Error("wrote into the subdirectory")
	}
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "**@ai annotations**") {
		t.Error(".hyp/comment.yaml at the module root was not used")
	}

	if err := runHyp(t, "ai-rules", "clean", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "AGENTS.md")) {
		t.Error("clean from a subdirectory should use the module root")
	}
}

func TestAIRulesNeedsAProject(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := runHyp(t, "ai-rules"); err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("err = %v", err)
	}
	if exists(filepath.Join(dir, "CLAUDE.md")) {
		t.Error("generated outside a project")
	}
}

// context.yaml 損壞時回錯，不要默默生成沒有路由表的規則
func TestAIRulesInvalidManifestFails(t *testing.T) {
	root := airulesProject(t)
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
	os.WriteFile(filepath.Join(root, ".hyp", "context.yaml"), []byte("routes: [\n"), 0o644)

	err := runHyp(t, "ai-rules", "--only", "agents")
	if err == nil || !strings.Contains(err.Error(), "context.yaml") {
		t.Fatalf("err = %v", err)
	}
	if exists(filepath.Join(root, "AGENTS.md")) {
		t.Error("generated despite the invalid manifest")
	}

	// 沒有 manifest 不是錯誤
	os.Remove(filepath.Join(root, ".hyp", "context.yaml"))
	if err := runHyp(t, "ai-rules", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
}

func TestAIRulesCommentAllOffIsRespected(t *testing.T) {
	root := airulesProject(t)
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
	os.WriteFile(filepath.Join(root, ".hyp", "comment.yaml"), []byte("normal_comment: false\nai_comment: false\nthink_comment: false\n"), 0o644)
	if err := runHyp(t, "ai-rules", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if strings.Contains(string(data), "**Comments** (REQUIRED)") {
		t.Error("comment rule generated although comment.yaml turns everything off")
	}
}

func TestAIRulesForceFlag(t *testing.T) {
	root := airulesProject(t)
	path := filepath.Join(root, "AGENTS.md")
	if err := runHyp(t, "ai-rules", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\nMY EDIT\n")
	f.Close()

	if err := runHyp(t, "ai-rules", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "MY EDIT") {
		t.Fatal("edited file overwritten without --force")
	}
	if err := runHyp(t, "ai-rules", "clean", "--only", "agents"); err != nil {
		t.Fatal(err)
	}
	if !exists(path) {
		t.Fatal("edited file removed without --force")
	}
	if err := runHyp(t, "ai-rules", "--only", "agents", "--force"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "MY EDIT") {
		t.Error("--force should overwrite the edit")
	}
}
