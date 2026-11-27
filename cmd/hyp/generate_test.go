package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// genProject 建立只有 go.mod 的專案；require 是要寫進 go.mod 的依賴
func genProject(t *testing.T, module string, require ...string) string {
	t.Helper()
	root := airulesProject(t)
	mod := "module " + module + "\n\ngo 1.24\n"
	for _, r := range require {
		mod += "\nrequire " + r + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeGenFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGenerateChecksProjectType：command / view / proto 只在對應類型的專案生成。
// 先前在 API 專案執行照樣成功（exit 0），之後 go build ./... 失敗：
// rootCmd 不存在、沒有 fyne、沒有 pb 套件
func TestGenerateChecksProjectType(t *testing.T) {
	root := genProject(t, "x", "github.com/uptrace/bun v1.2.17") // 有別的依賴，但不是這三種專案
	for _, tc := range []struct{ kind, want string }{
		{"command", "hyp new cli"}, {"view", "hyp new desktop"}, {"proto", "hyp new grpc"},
	} {
		err := runHyp(t, "generate", tc.kind, "export")
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "nothing was generated") {
			t.Errorf("generate %s in a project of another type: %v", tc.kind, err)
		}
	}
	if exists(filepath.Join(root, "app")) {
		t.Error("files were written although the project type does not match")
	}

	// 對應類型的專案照常生成
	root = genProject(t, "x", "fyne.io/fyne/v2 v2.8.1", "google.golang.org/grpc v1.80.0")
	writeGenFile(t, root, "app/commands/root.go", "package commands\n\nvar rootCmd = 1\n")
	for _, tc := range []struct{ kind, file string }{
		{"command", "app/commands/export.go"}, {"view", "app/views/export_view.go"}, {"proto", "app/rpc/export_server.go"},
	} {
		if err := runHyp(t, "generate", tc.kind, "export"); err != nil {
			t.Errorf("generate %s: %v", tc.kind, err)
		}
		if !exists(filepath.Join(root, filepath.FromSlash(tc.file))) {
			t.Errorf("%s missing", tc.file)
		}
	}
}

// TestGenerateControllerChecksExistingFiles：沿用既有的 model / service 前確認它有 controller 要用的名稱。
// 先前手寫的 app/models/user.go 只有 User 時照樣生成，service 與 controller 引用不存在的 models.UserResp
func TestGenerateControllerChecksExistingFiles(t *testing.T) {
	root := genProject(t, "x")
	writeGenFile(t, root, "app/models/user.go", "package models\n\ntype User struct{}\n")
	err := runHyp(t, "generate", "controller", "user")
	if err == nil || !strings.Contains(err.Error(), "UserResp") || !strings.Contains(err.Error(), "nothing was generated") {
		t.Fatalf("incomplete model: %v", err)
	}
	if strings.Contains(err.Error(), " User,") {
		t.Errorf("User is declared and should not be listed: %v", err)
	}
	for _, rel := range []string{"app/services", "app/controllers", "routers"} {
		if exists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Errorf("%s was written although generation failed", rel)
		}
	}

	// 需要的名稱宣告在同套件的另一個檔案也算
	writeGenFile(t, root, "app/models/user_dto.go", "package models\n\ntype (\n\tCreateUserReq struct{}\n\tUpdateUserReq struct{}\n\tUserResp struct{}\n\tUserListResp struct{}\n)\n")
	// 既有的 service 少了錯誤碼
	writeGenFile(t, root, "app/services/user_service.go", "package services\n\ntype UserService struct{}\n\nfunc NewUserService() *UserService { return nil }\n")
	err = runHyp(t, "generate", "controller", "user")
	if err == nil || !strings.Contains(err.Error(), "ErrUserNotImplemented") || !strings.Contains(err.Error(), "user_service.go") {
		t.Fatalf("incomplete service: %v", err)
	}
	os.Remove(filepath.Join(root, "app", "services", "user_service.go"))
	if err := runHyp(t, "generate", "controller", "user"); err != nil {
		t.Fatalf("a complete model should be reused: %v", err)
	}
}

// TestGenerateControllerReservedNames：router / middleware 的資源檔會佔掉 routers/router.go、
// routers/middleware.go。先前專案還沒有 routers/ 時照樣生成，Setup 不存在而完成訊息要人「add to Setup」
func TestGenerateControllerReservedNames(t *testing.T) {
	root := genProject(t, "x")
	for _, name := range []string{"router", "Middleware"} {
		if err := runHyp(t, "generate", "controller", name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if exists(filepath.Join(root, "app")) || exists(filepath.Join(root, "routers")) {
		t.Error("files were written for a reserved name")
	}
}

// TestGenerateModuleFlagMustMatchGoMod：--module 與 go.mod 不同時回錯。
// 先前照寫進 import，生成的程式碼一定編不過
func TestGenerateModuleFlagMustMatchGoMod(t *testing.T) {
	root := genProject(t, "example.com/shop")
	t.Cleanup(func() { generateCmd.Flags().Set("module", "") })
	err := runHyp(t, "generate", "model", "item", "-m", "example.com/other")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("mismatched --module: %v", err)
	}
	if exists(filepath.Join(root, "app")) {
		t.Error("files were written with a mismatched --module")
	}
	if err := runHyp(t, "generate", "model", "item", "-m", "example.com/shop"); err != nil {
		t.Errorf("--module equal to go.mod should be accepted: %v", err)
	}
}

// TestGenerateCommandRootNameIgnoresMajorVersion：根命令的名稱不含 module 路徑結尾的 /v2。
// 先前 example.com/tools/v2 誤擋 v2、放行 tools，後者讓程式啟動即 panic（Schema 重複登錄）
func TestGenerateCommandRootNameIgnoresMajorVersion(t *testing.T) {
	root := genProject(t, "example.com/tools/v2")
	writeGenFile(t, root, "app/commands/root.go", "package commands\n\nvar rootCmd = 1\n")
	if err := runHyp(t, "generate", "command", "tools"); err == nil || !strings.Contains(err.Error(), "root command") {
		t.Errorf("the root command name should be refused: %v", err)
	}
	if err := runHyp(t, "generate", "command", "v2"); err != nil {
		t.Errorf("v2 is not the root command: %v", err)
	}
}

// TestGenerateUsageMessage：參數數量不對時列出可用的類型（先前只有 "accepts 2 arg(s), received 1"）
func TestGenerateUsageMessage(t *testing.T) {
	genProject(t, "x")
	for _, args := range [][]string{{"generate"}, {"generate", "controller"}, {"generate", "model", "a", "b"}} {
		err := runHyp(t, args...)
		if err == nil || !strings.Contains(err.Error(), "controller, model, service") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// TestGenerateMessages：完成訊息說明 go mod tidy、注入要改的三處、view 要補登 Schema
func TestGenerateMessages(t *testing.T) {
	genProject(t, "x", "fyne.io/fyne/v2 v2.8.1")
	out := captureStdout(t, func() {
		if err := runHyp(t, "generate", "controller", "order"); err != nil {
			t.Fatal(err)
		}
		if err := runHyp(t, "generate", "view", "settings"); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"go mod tidy", "Setup(r, db, log)", "RegisterOrderRoutes(r, db, log)", "services.NewOrderService(db, log)", `RegisterDesktop("view/settings"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// captureStdout 收集 f 執行期間寫到 os.Stdout 的內容
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	f()
	os.Stdout = old
	w.Close()
	return <-done
}
