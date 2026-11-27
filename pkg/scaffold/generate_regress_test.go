package scaffold

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aiProject 建立開啟 ai_comment 的專案目錄
func aiProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".hyp"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hyp", "comment.yaml"), []byte("ai_comment: true\n"), 0o644)
	return dir
}

// TestGRPCServerIsUnimplemented：未實作的 gRPC 方法回 Unimplemented。
// 先前回寫死的假資料（Get 一律 "example"、Delete 一律 Success: true）並以 fmt.Printf 印請求
func TestGRPCServerIsUnimplemented(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateProto(dir, "order", "x"); err != nil {
		t.Fatal(err)
	}
	src := readGen(t, filepath.Join(dir, "app", "rpc", "order_server.go"))
	if n := strings.Count(src, "codes.Unimplemented"); n != 5 {
		t.Errorf("want 5 methods answering Unimplemented, got %d", n)
	}
	for _, bad := range []string{`"example"`, "Success: true", "fmt.Printf", "Id:"} {
		if strings.Contains(src, bad) {
			t.Errorf("server still contains %s", bad)
		}
	}
}

// TestGRPCListUsesPlural：List 方法用複數規則（先前直接加 s：ListCategorys）
func TestGRPCListUsesPlural(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateProto(dir, "category", "x"); err != nil {
		t.Fatal(err)
	}
	proto := readGen(t, filepath.Join(dir, "app", "proto", "categorypb", "category.proto"))
	server := readGen(t, filepath.Join(dir, "app", "rpc", "category_server.go"))
	for name, src := range map[string]string{"proto": proto, "server": server} {
		if !strings.Contains(src, "ListCategories") || strings.Contains(src, "ListCategorys") {
			t.Errorf("%s should use ListCategories", name)
		}
	}
	if !strings.Contains(proto, "rpc ListCategories (ListCategoriesRequest) returns (ListCategoriesResponse);") {
		t.Error("proto rpc line is wrong")
	}
}

// TestGenerateProtoWritesNothingWhenServerExists：先前 .proto 寫完才發現 server 檔已存在，留下半套
func TestGenerateProtoWritesNothingWhenServerExists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "app", "rpc"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "rpc", "order_server.go"), []byte("package rpc\n"), 0o644)
	if err := GenerateProto(dir, "order", "x"); err == nil {
		t.Fatal("want an error when the server file exists")
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "proto", "orderpb", "order.proto")); err == nil {
		t.Error("the .proto was written although generation failed")
	}
}

// TestCommandViewProtoAreDocumented：command / view / proto 的生成檔有套件註解，匯出的宣告有 doc 註解，
// ai_comment 開啟時帶 @ai 區塊。先前這三個模板沒有註解：ai_comment: true 的專案生成後過不了 hyp chkcomment，
// gRPC 專案在預設設定下也有 5 個違規
func TestCommandViewProtoAreDocumented(t *testing.T) {
	dir := aiProject(t)
	if err := GenerateCommand(filepath.Join(dir, "app", "commands"), "sync"); err != nil {
		t.Fatal(err)
	}
	if err := GenerateView(filepath.Join(dir, "app", "views"), "settings"); err != nil {
		t.Fatal(err)
	}
	if err := GenerateProto(dir, "order", "x"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"app/commands/sync.go", "app/views/settings_view.go", "app/rpc/order_server.go"} {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if f.Doc == nil || !strings.Contains(f.Doc.Text(), "@ai:purpose") {
			t.Errorf("%s: package doc with @ai:purpose missing", rel)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Doc == nil {
					t.Errorf("%s: %s has no doc comment", rel, d.Name.Name)
					continue
				}
				for _, tag := range []string{"@ai:purpose", "@ai:input", "@ai:output", "@ai:sideeffect"} {
					if !strings.Contains(d.Doc.Text(), tag) {
						t.Errorf("%s: %s lacks %s", rel, d.Name.Name, tag)
					}
				}
			case *ast.GenDecl:
				if d.Tok == token.TYPE && (d.Doc == nil || !strings.Contains(d.Doc.Text(), "@ai:purpose")) {
					t.Errorf("%s: type declaration lacks a doc comment with @ai:purpose", rel)
				}
			}
		}
	}

	// ai_comment 關閉（預設）時不帶 @ai，一般註解仍在
	plain := t.TempDir()
	if err := GenerateProto(plain, "order", "x"); err != nil {
		t.Fatal(err)
	}
	src := readGen(t, filepath.Join(plain, "app", "rpc", "order_server.go"))
	if strings.Contains(src, "@ai:") || !strings.Contains(src, "// CreateOrder 建立新的 order。") {
		t.Errorf("default output should keep plain doc comments only:\n%s", src)
	}
}

// TestAICommentLookupStopsAtModuleRoot：comment.yaml 只找到 module 根目錄。
// 先前繼續往上，外層目錄的 .hyp/comment.yaml 開了 ai_comment 就影響裡面的專案
func TestAICommentLookupStopsAtModuleRoot(t *testing.T) {
	outer := aiProject(t)
	inner := filepath.Join(outer, "inner")
	os.MkdirAll(inner, 0o755)
	os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module inner\n"), 0o644)
	if err := GenerateModel(filepath.Join(inner, "app", "models"), "thing"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readGen(t, filepath.Join(inner, "app", "models", "thing.go")), "@ai:") {
		t.Error("the outer project's comment.yaml was applied to the inner module")
	}
	// 專案自己的設定照常生效
	if err := GenerateModel(filepath.Join(outer, "app", "models"), "thing"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readGen(t, filepath.Join(outer, "app", "models", "thing.go")), "@ai:") {
		t.Error("the project's own comment.yaml should apply")
	}
}

// TestRouterTemplateExplainsInjection：注入 service 要改的三處寫在生成的路由檔裡。
// 先前只有一行 services.NewXService(db, log)，而這個函式與 Setup 都拿不到 db / log
func TestRouterTemplateExplainsInjection(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateRouter(dir, "order", "x"); err != nil {
		t.Fatal(err)
	}
	src := readGen(t, filepath.Join(dir, "order.go"))
	for _, want := range []string{"routers.Setup(app.Server().Router(), db, log)", "RegisterOrderRoutes(r, db, log)", `import "x/app/services"`} {
		if !strings.Contains(src, want) {
			t.Errorf("router lacks %q", want)
		}
	}
}

func TestMissingDecls(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n\ntype A struct{}\n\nfunc (A) Method() {}\n\nvar v, W = 1, 2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package p\n\nfunc F() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a_test.go"), []byte("package p\n\ntype OnlyInTest struct{}\n"), 0o644)
	got := strings.Join(MissingDecls(dir, "A", "F", "v", "W", "Method", "OnlyInTest", "Nope"), ",")
	if got != "Method,OnlyInTest,Nope" {
		t.Errorf("MissingDecls = %s", got)
	}
	if got := MissingDecls(filepath.Join(dir, "absent"), "X"); len(got) != 1 {
		t.Errorf("a missing directory declares nothing, got %v", got)
	}
}
