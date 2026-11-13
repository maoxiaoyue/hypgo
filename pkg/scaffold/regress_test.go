package scaffold

import (
	"go/format"
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

// readGen 讀取生成的檔案
func readGen(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestGRPCNamesMatchProtocGen 迴歸測試（A1）：名稱含底線時，Go 程式碼引用的型別與 protoc-gen-go 產生的一致。
// 歷史 bug：proto 寫 CreateUser_svcRequest，protoc-gen-go 產生 CreateUserSvcRequest，
// 模板卻引用 pb.CreateUser_svcRequest，專案編譯不過
func TestGRPCNamesMatchProtocGen(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateGRPCProject(dir, "user_svc", "m"); err != nil {
		t.Fatal(err)
	}
	proto := readGen(t, filepath.Join(dir, "app", "proto", "user_svcpb", "user_svc.proto"))
	server := readGen(t, filepath.Join(dir, "app", "rpc", "user_svc_server.go"))
	for _, want := range []string{"service UserSvcService", "message CreateUserSvcRequest"} {
		if !strings.Contains(proto, want) {
			t.Errorf("proto missing %q", want)
		}
	}
	for _, want := range []string{"pb.UnimplementedUserSvcServiceServer", "*pb.CreateUserSvcRequest"} {
		if !strings.Contains(server, want) {
			t.Errorf("server missing %q", want)
		}
	}
}

// TestGeneratedGoIsGofmtClean 迴歸測試（A2）：生成的 .go 檔一律通過 gofmt。
// 歷史 bug：模板的欄位對齊寫死，名稱長度不同時（api、type、gRPC server）過不了 gofmt -l
func TestGeneratedGoIsGofmtClean(t *testing.T) {
	dir := t.TempDir()
	for _, err := range []error{
		GenerateModel(dir, "api"), GenerateModel(dir, "user_profile"),
		GenerateController(dir, "api", "m"), GenerateService(dir, "api", "m"),
		GenerateGRPCProject(filepath.Join(dir, "g"), "demo", ""),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		src := readGen(t, p)
		if formatted, ferr := format.Source([]byte(src)); ferr != nil || string(formatted) != src {
			t.Errorf("%s is not gofmt-clean (err=%v)", p, ferr)
		}
		return nil
	})
}

// TestGenerateFileRejectsInvalidGo 迴歸測試（B1）：模板產出不合法的 Go 時回錯，且不留下檔案。
// 歷史 bug：直接寫出壞檔且回報成功；寫入中途失敗時留下半截檔案，重跑只得到 "file already exists"
func TestGenerateFileRejectsInvalidGo(t *testing.T) {
	dir := t.TempDir()
	if err := generateFile(dir, "bad.go", "package x\nfunc {", nil); err == nil {
		t.Error("generateFile accepted invalid Go")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.go")); err == nil {
		t.Error("invalid file was left on disk")
	}
}

// TestModelTableAndAlias 迴歸測試（B2、B3）：資料表名依英文複數規則，alias 不再只取第一個字母。
// 歷史 bug：一律加 s（categorys、boxs）；Category 與 Comment 的 alias 都是 "c"，一起 join 時查詢出錯
func TestModelTableAndAlias(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]string{
		"category": `table:categories,alias:category`, "box": `table:boxes,alias:box`,
		"comment": `table:comments,alias:comment`, "day": `table:days,alias:day`,
	} {
		if err := GenerateModel(dir, name); err != nil {
			t.Fatal(err)
		}
		if src := readGen(t, filepath.Join(dir, name+".go")); !strings.Contains(src, want) {
			t.Errorf("%s: want %q", name, want)
		}
	}
}

// TestCamelCaseIdentifiers 迴歸測試（B4）：底線名稱產生 CamelCase 識別字，路徑與檔名維持原樣。
// 歷史 bug：只把首字母大寫，產生 User_profileController、ErrUser_profileNotFound
func TestCamelCaseIdentifiers(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateController(dir, "user_profile", "m"); err != nil {
		t.Fatal(err)
	}
	if err := GenerateRouter(dir, "user_profile", "m"); err != nil {
		t.Fatal(err)
	}
	ctrl := readGen(t, filepath.Join(dir, "user_profile_controller.go"))
	if !strings.Contains(ctrl, "type UserProfileController struct") || strings.Contains(ctrl, "User_profile") {
		t.Errorf("controller identifiers not CamelCase:\n%s", ctrl)
	}
	if r := readGen(t, filepath.Join(dir, "user_profile.go")); !strings.Contains(r, `"/api/user_profiles"`) {
		t.Error("route path should be the plural of the original name")
	}
}

// TestGoModUsesHypgoVersion 迴歸測試（B5）：CLI / desktop / gRPC 的 go.mod 使用 HypgoVersion。
// 歷史 bug：模板寫死 v0.9.2，升版時遺漏
func TestGoModUsesHypgoVersion(t *testing.T) {
	old := HypgoVersion
	HypgoVersion = "v9.9.9"
	defer func() { HypgoVersion = old }()
	dir := t.TempDir()
	for name, gen := range map[string]func(string, string, string) error{
		"cli": GenerateCLIProject, "desktop": GenerateDesktopProject, "grpc": GenerateGRPCProject,
	} {
		d := filepath.Join(dir, name)
		if err := gen(d, "demo", ""); err != nil {
			t.Fatal(err)
		}
		if src := readGen(t, filepath.Join(d, "go.mod")); !strings.Contains(src, "github.com/maoxiaoyue/hypgo v9.9.9") {
			t.Errorf("%s go.mod does not use HypgoVersion:\n%s", name, src)
		}
	}
}

// TestGenerateRouterSetupValidatesName 迴歸測試（B6）：GenerateRouterSetup 驗證名稱。
// 歷史 bug：唯一未驗證的產生器，含換行的名稱等於在生成的程式碼裡插入任意內容
func TestGenerateRouterSetupValidatesName(t *testing.T) {
	if err := GenerateRouterSetup(t.TempDir(), "x\nfunc init() { panic(1) }\n//", "m"); err == nil {
		t.Error("GenerateRouterSetup accepted a name containing newlines")
	}
}

// TestAICommentFollowsCommentYAML 迴歸測試（C1、C2）：@ai 區塊依目標專案的 comment.yaml 輸出，by= 固定為 hypgo-scaffold。
// 歷史 bug：不論 ai_comment 一律輸出完整 @ai 區塊；by= 依 llm.yaml 或掃描 *.md 關鍵字猜測
// （README 提到 OpenAI 就標 by=openai），但這些程式碼出自模板
func TestAICommentFollowsCommentYAML(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile("README.md", []byte("Works with any OpenAI-compatible API"), 0644)

	off := t.TempDir()
	if err := GenerateController(off, "item", "m"); err != nil {
		t.Fatal(err)
	}
	if src := readGen(t, filepath.Join(off, "item_controller.go")); strings.Contains(src, "@ai:") {
		t.Error("ai_comment off (no comment.yaml): @ai annotations were emitted")
	}

	on := projectWithAIComment(t, "controllers")
	if err := GenerateController(on, "item", "m"); err != nil {
		t.Fatal(err)
	}
	src := readGen(t, filepath.Join(on, "item_controller.go"))
	if !strings.Contains(src, "@ai:generated by=hypgo-scaffold") || strings.Contains(src, "by=openai") {
		t.Errorf("ai_comment on: want by=hypgo-scaffold annotations:\n%s", src)
	}
}

// TestLLMYamlTemplateMatchesExample 迴歸測試（C3）：LLMYamlTemplate 與 pkg/config/llm_example.yaml 一致。
// 歷史 bug：註解說兩者同步，實際模板多了一段 timeout 說明
func TestLLMYamlTemplateMatchesExample(t *testing.T) {
	if src := readGen(t, filepath.Join("..", "config", "llm_example.yaml")); src != LLMYamlTemplate {
		t.Error("pkg/config/llm_example.yaml differs from LLMYamlTemplate")
	}
}

// 專案名稱允許連字號；識別字、套件名與檔名改用底線形式。
// 歷史 bug：與資源名稱共用規則，hyp new cli my-tool 被拒絕，hyp new my-app 卻可以
func TestProjectNamesWithHyphens(t *testing.T) {
	for name, ok := range map[string]bool{
		"my-tool": true, "user_service": true, "app2": true,
		"": false, "-lead": false, "my tool": false, "a/b": false, "..": false, "my.app": false,
	} {
		if err := ValidateProjectName(name); (err == nil) != ok {
			t.Errorf("ValidateProjectName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
	// 資源名稱（hyp generate）仍不接受連字號
	if err := ValidateName("order-item"); err == nil {
		t.Error("resource names should still reject hyphens")
	}
	if got := CamelName("user-service"); got != "UserService" {
		t.Errorf("CamelName = %q", got)
	}

	dir := filepath.Join(t.TempDir(), "user-service")
	if err := GenerateGRPCProject(dir, "user-service", "user-service"); err != nil {
		t.Fatal(err)
	}
	proto, err := os.ReadFile(filepath.Join(dir, "app", "proto", "user_servicepb", "user_service.proto"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package user_servicepb;", `go_package = "user-service/app/proto/user_servicepb"`, "service UserService {"} {
		if !strings.Contains(string(proto), want) {
			t.Errorf("proto missing %q", want)
		}
	}
	mk, _ := os.ReadFile(filepath.Join(dir, "Makefile"))
	if !strings.Contains(string(mk), "bin/user-service") {
		t.Error("binary should be named after the project")
	}
}

// 歷史 bug：只有 hyp api 產生 .gitignore
func TestProjectsHaveGitignore(t *testing.T) {
	gens := map[string]func(string, string, string) error{
		"cli": GenerateCLIProject, "desktop": GenerateDesktopProject, "grpc": GenerateGRPCProject,
	}
	for kind, gen := range gens {
		dir := filepath.Join(t.TempDir(), "demo")
		if err := gen(dir, "demo", "demo"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Errorf("%s: no .gitignore", kind)
			continue
		}
		for _, want := range []string{"hypgo.pid", "*.bak", "logs/*", "*.key"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s: .gitignore missing %s", kind, want)
			}
		}
	}
}

// KeepExisting：既有檔案保留並回報，其餘照常生成。
// 歷史 bug：遇到第一個既有檔案就回錯，留下寫到一半的專案
func TestKeepExistingSkipsFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("mine\n"), 0o644)

	if err := GenerateCLIProject(dir, "demo", "demo"); err == nil {
		t.Fatal("an existing file should be an error by default")
	}

	var kept []string
	KeepExisting, OnKeep = true, func(p string) { kept = append(kept, filepath.Base(p)) }
	defer func() { KeepExisting, OnKeep = false, nil }()
	if err := GenerateCLIProject(dir, "demo", "demo"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(data) != "mine\n" {
		t.Error("existing file was overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Error("project was not completed")
	}
	if !strings.Contains(strings.Join(kept, " "), ".gitignore") {
		t.Errorf("kept = %v", kept)
	}
}
