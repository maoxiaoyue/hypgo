package impact

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const testModule = "module example.com/project\n\ngo 1.24\n"

// moduleProject 建立含 go.mod 的專案：app → svc → core，另有只在測試 import core 的 tool
func moduleProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "pkg/core/core.go", `package core

// Hello is used by svc.
func Hello() string { return "hello" }

// Unused is not referenced by other packages.
func Unused() {}

// Item is a type.
type Item struct{ ID int }

// Get is a method.
func (i *Item) Get() int { return i.ID }

const Version = "1"
`)
	writeFile(t, root, "pkg/core/other.go", "package core\n\nfunc Other() {}\n")
	writeFile(t, root, "pkg/core/core_test.go", `package core

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(m.Run()) }
func TestHello(t *testing.T) {}
func Testify() {}
func TestWrongSignature() {}
func Testlower(t *testing.T) {}
`)
	writeFile(t, root, "pkg/svc/svc.go", `package svc

import c "example.com/project/pkg/core"

func Call() string {
	var it c.Item
	_ = it
	return c.Hello() + c.Version
}
`)
	writeFile(t, root, "pkg/svc/svc_test.go", "package svc\n\nimport \"testing\"\n\nfunc TestCall(t *testing.T) {}\n")
	writeFile(t, root, "app/app.go", `package app

import "example.com/project/pkg/svc"

func Run() string { return svc.Call() }
`)
	writeFile(t, root, "tool/tool.go", "package tool\n")
	writeFile(t, root, "tool/tool_test.go", `package tool

import (
	"testing"

	"example.com/project/pkg/core"
)

func TestTool(t *testing.T) { core.Other() }
`)
	return root
}

func mustAnalyze(t *testing.T, root, rel string) *ImpactReport {
	t.Helper()
	r, err := Analyze(filepath.Join(root, filepath.FromSlash(rel)), root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func dependentPaths(r *ImpactReport) string {
	var out []string
	for _, d := range r.Dependents {
		out = append(out, d.Path)
	}
	return strings.Join(out, ",")
}

// TestAnalyzeWithDotRoot 迴歸測試：CLI 以 "." 當根目錄時要找得到依賴者。
// 歷史 bug：走訪第一步的目錄名就是 "."，被當成隱藏目錄跳過，hyp impact 對任何檔案都回報沒有依賴者
func TestAnalyzeWithDotRoot(t *testing.T) {
	root := moduleProject(t)
	t.Chdir(root)
	r, err := Analyze("pkg/core/core.go", ".")
	if err != nil {
		t.Fatal(err)
	}
	if got := dependentPaths(r); got != "pkg/svc,tool" {
		t.Errorf("dependents = %q", got)
	}
}

// TestAnalyzeRejectsInvalidTargets 迴歸測試：不存在或不是 .go 的目標要回錯。
// 歷史 bug：回報「沒有依賴者、LOW」並 exit 0，等於告訴 AI 可以放心改（打錯路徑也一樣）
func TestAnalyzeRejectsInvalidTargets(t *testing.T) {
	root := moduleProject(t)
	for _, target := range []string{"pkg/core/nope.go", "pkg/core", "go.mod"} {
		if _, err := Analyze(filepath.Join(root, filepath.FromSlash(target)), root); err == nil {
			t.Errorf("Analyze(%s) should fail", target)
		}
	}
}

// TestAnalyzeRejectsSiblingWithSamePrefix 迴歸測試：同前綴的相鄰目錄不算在專案內。
// 歷史 bug：以字串前綴判斷，/proj2/x.go 會通過 /proj 的檢查
func TestAnalyzeRejectsSiblingWithSamePrefix(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "proj2/x.go", "package x\n")
	if _, err := Analyze(filepath.Join(base, "proj2", "x.go"), filepath.Join(base, "proj")); err == nil {
		t.Error("a sibling directory sharing the prefix was accepted")
	}
}

// TestDependentsIgnoreExternalPackages 迴歸測試：外部套件不會因為路徑結尾相同就算成依賴者。
// 歷史 bug：pkg/grpc 把 import google.golang.org/grpc 的檔案都算進來，
// pkg/errors 把 github.com/pkg/errors 算進來
func TestDependentsIgnoreExternalPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "pkg/grpc/server.go", "package grpc\n")
	writeFile(t, root, "pkg/errors/errors.go", "package errors\n")
	writeFile(t, root, "pkg/errors/more/more.go", "package more\n")
	writeFile(t, root, "other/other.go", `package other

import (
	_ "github.com/pkg/errors"
	_ "google.golang.org/grpc"
	_ "example.com/project/pkg/errors/more"
)
`)
	for _, target := range []string{"pkg/grpc/server.go", "pkg/errors/errors.go"} {
		if got := dependentPaths(mustAnalyze(t, root, target)); got != "" {
			t.Errorf("%s: dependents = %q, want none", target, got)
		}
	}
	if got := dependentPaths(mustAnalyze(t, root, "pkg/errors/more/more.go")); got != "other" {
		t.Errorf("more: dependents = %q", got)
	}
}

// TestRootPackageDependents 迴歸測試：根套件也有依賴者。
// 歷史 bug：根套件的路徑是 "."，永遠比對不到，回報沒有依賴者
func TestRootPackageDependents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "project.go", "package project\n")
	writeFile(t, root, "cmd/x/main.go", "package main\n\nimport _ \"example.com/project\"\n\nfunc main() {}\n")
	r := mustAnalyze(t, root, "project.go")
	if got := dependentPaths(r); got != "cmd/x" {
		t.Errorf("dependents = %q", got)
	}
	if r.Package != "." {
		t.Errorf("Package = %q", r.Package)
	}
}

// TestSkipsTestdataAndNestedModules 迴歸測試：testdata 與巢狀 module 不算依賴者。
// 歷史 bug：pkg/lintdeep/testdata/sample 被列為 pkg/context 的依賴者
func TestSkipsTestdataAndNestedModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "core/core.go", "package core\n")
	imports := "package x\n\nimport _ \"example.com/project/core\"\n"
	writeFile(t, root, "lint/testdata/sample/x.go", imports)
	writeFile(t, root, "_old/x.go", imports)
	writeFile(t, root, "sub/go.mod", "module example.com/project/sub\n")
	writeFile(t, root, "sub/x.go", imports)
	if got := dependentPaths(mustAnalyze(t, root, "core/core.go")); got != "" {
		t.Errorf("dependents = %q, want none", got)
	}
}

// TestOutputIsDeterministic 迴歸測試：輸出順序固定。
// 歷史 bug：依 map 迭代順序輸出，每次都不同，也無法拿來比對或放進 prompt cache
func TestOutputIsDeterministic(t *testing.T) {
	root := moduleProject(t)
	first := FormatReport(mustAnalyze(t, root, "pkg/core/core.go"))
	for i := 0; i < 15; i++ {
		if got := FormatReport(mustAnalyze(t, root, "pkg/core/core.go")); got != first {
			t.Fatalf("output differs between runs:\n%s\n---\n%s", first, got)
		}
	}
}

// TestCountsOnlyRealTests 迴歸測試：只算 go test 真正會跑的函式。
// 歷史 bug：只看 Test 前綴，TestMain、Testify、簽名不對的函式都算進去
func TestCountsOnlyRealTests(t *testing.T) {
	root := moduleProject(t)
	if got := countTestsInPackage(root, "pkg/core"); got != 1 {
		t.Errorf("count = %d, want 1 (only TestHello)", got)
	}
}

// 間接依賴列出經由哪個套件；只有測試 import 的不往上傳遞
func TestTransitiveDependents(t *testing.T) {
	r := mustAnalyze(t, moduleProject(t), "pkg/core/core.go")
	if len(r.Transitive) != 1 || r.Transitive[0].Path != "app" || r.Transitive[0].Via != "pkg/svc" {
		t.Errorf("Transitive = %+v", r.Transitive)
	}
	var tool DependentFile
	for _, d := range r.Dependents {
		if d.Path == "tool" {
			tool = d
		}
	}
	if !tool.TestOnly {
		t.Errorf("tool should be test-only: %+v", tool)
	}
}

func TestAPIUsage(t *testing.T) {
	root := moduleProject(t)
	// 區域變數遮蔽套件名：不是對套件的引用
	writeFile(t, root, "pkg/shadow/shadow.go", `package shadow

import "example.com/project/pkg/core"

type fake struct{ Hello int }

func F() int {
	core := fake{}
	return core.Hello
}

var _ = core.Version
`)
	r := mustAnalyze(t, root, "pkg/core/core.go")
	got := map[string]SymbolUsage{}
	for _, s := range r.API {
		got[s.Name] = s
	}
	if hello := got["Hello"]; hello.Uses != 1 || strings.Join(hello.Packages, ",") != "pkg/svc" || hello.Kind != kindFunc {
		t.Errorf("Hello = %+v (alias c, and shadowed core.Hello must not count)", hello)
	}
	if v := got["Version"]; v.Uses != 2 || v.Kind != kindConst {
		t.Errorf("Version = %+v", v)
	}
	if item := got["Item"]; item.Uses != 1 || item.Kind != kindType || strings.Join(item.Methods, ",") != "Get" {
		t.Errorf("Item = %+v", item)
	}
	if u := got["Unused"]; u.Uses != 0 || !u.Tracked {
		t.Errorf("Unused = %+v", u)
	}
	if _, ok := got["Other"]; ok {
		t.Error("Other is declared in another file and should not be listed")
	}
	for _, d := range r.Dependents {
		if d.Path == "pkg/svc" && strings.Join(d.Uses, ",") != "Hello,Item,Version" {
			t.Errorf("svc uses = %v", d.Uses)
		}
	}
}

func TestMethodOnTypeDeclaredElsewhereIsUntracked(t *testing.T) {
	root := moduleProject(t)
	writeFile(t, root, "pkg/core/methods.go", "package core\n\nfunc (i Item) Extra() {}\n")
	r := mustAnalyze(t, root, "pkg/core/methods.go")
	if len(r.API) != 1 || r.API[0].Name != "Item.Extra" || r.API[0].Tracked {
		t.Errorf("API = %+v", r.API)
	}
}

const manifestYAML = `routes:
  - method: POST
    path: /users
    handler_names: [controllers.(*UserController).Create]
    input_type: CreateReq
    output_type: UserResp
  - method: GET
    path: /users
    handler_names: [controllers.(*UserController).List]
    output_type: "[]UserResp"
  - method: GET
    path: /ping
    handler_names: [routers.Setup.func1]
  - method: GET
    path: /other
    handler_names: [elsewhere.Other]
    response_types: {404: UserResp}
  - protocol: grpc
    command: UserService/Get
    handler_names: [controllers.Get]
`

func routesProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, ".hyp/context.yaml", manifestYAML)
	writeFile(t, root, "app/models/user.go", "package models\n\ntype UserResp struct{}\n\ntype CreateReq struct{}\n")
	writeFile(t, root, "app/controllers/user.go", `package controllers

import _ "example.com/project/app/models"

type UserController struct{}

func (c *UserController) Create() {}
func (c *UserController) List()   {}
func Get()                        {}
`)
	writeFile(t, root, "routers/router.go", "package routers\n\nimport _ \"example.com/project/app/controllers\"\n\nfunc Setup() {}\n")
	return root
}

func routeMap(r *ImpactReport) map[string]RouteImpact {
	out := map[string]RouteImpact{}
	for _, route := range r.Routes {
		out[route.Route] = route
	}
	return out
}

func TestRoutesFromDeclaredTypes(t *testing.T) {
	r := mustAnalyze(t, routesProject(t), "app/models/user.go")
	if !r.RoutesKnown {
		t.Fatal("RoutesKnown = false")
	}
	got := routeMap(r)
	for route, want := range map[string]string{
		"POST /users": "input type CreateReq",
		"GET /users":  "output type []UserResp",
		"GET /other":  "response 404 type UserResp",
	} {
		if !got[route].Direct || got[route].Reason != want {
			t.Errorf("%s = %+v, want direct %q", route, got[route], want)
		}
	}
	// controllers 依賴 models：其他路由經由受影響的套件
	if g := got["grpc UserService/Get"]; g.Route == "" || g.Direct {
		t.Errorf("grpc route = %+v, want indirect", g)
	}
	if !r.Routes[0].Direct {
		t.Error("direct routes should come first")
	}
}

func TestRoutesFromHandlers(t *testing.T) {
	root := routesProject(t)
	got := routeMap(mustAnalyze(t, root, "app/controllers/user.go"))
	if g := got["POST /users"]; !g.Direct || !strings.Contains(g.Reason, "(*UserController).Create") {
		t.Errorf("POST /users = %+v", g)
	}
	if g := got["grpc UserService/Get"]; !g.Direct {
		t.Errorf("function handler = %+v", g)
	}
	// 閉包 routers.Setup.func1 屬於 Setup 所在的檔案
	got = routeMap(mustAnalyze(t, root, "routers/router.go"))
	if g := got["GET /ping"]; !g.Direct {
		t.Errorf("closure handler = %+v", g)
	}
	if _, ok := got["POST /users"]; ok {
		t.Error("routers depends on controllers, not the other way round: POST /users is unaffected")
	}
}

// 註冊路由的檔案本身就是直接影響。實測 hyp api 專案時 routers/router.go 回報「沒有路由受影響」：
// handler 在 controllers，而 routers 依賴 controllers，只比對 handler 永遠看不到這個方向
func TestRoutesFromRegistrations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, ".hyp/context.yaml", `routes:
  - method: GET
    path: /api/users
    handler_names: [controllers.List]
  - method: POST
    path: /api/users
    handler_names: [controllers.Create]
  - method: GET
    path: /health
    handler_names: [controllers.Health]
  - method: GET
    path: /unrelated
    handler_names: [elsewhere.X]
  - method: POST
    path: /api/admin-users
    handler_names: [elsewhere.Y]
  - protocol: grpc
    command: Svc/Get
`)
	writeFile(t, root, "app/controllers/c.go", "package controllers\n\nfunc List() {}\nfunc Create() {}\nfunc Health() {}\n")
	writeFile(t, root, "routers/users.go", `package routers

import (
	"example.com/project/app/controllers"
	"example.com/project/pkg/schema"
)

func RegisterUsers(g group) {
	g.Schema(schema.Route{Method: "get", Path: "/users"}).Handle(controllers.List)
	g.POST("/users", controllers.Create)
}

type group interface {
	Schema(schema.Route) handle
	POST(string, func())
}

type handle interface{ Handle(func()) }
`)
	writeFile(t, root, "routers/setup.go", `package routers

import "example.com/project/app/controllers"

func Setup(r router) {
	r.GET("/health", controllers.Health)
}

type router interface{ GET(string, func()) }
`)
	writeFile(t, root, "rpc/schema.go", "package rpc\n\nimport \"example.com/project/pkg/schema\"\n\nfunc Reg() {\n\tschema.RegisterGRPC(\"Svc/Get\", \"s\", nil, nil)\n}\n")
	writeFile(t, root, "pkg/schema/s.go", "package schema\n\ntype Route struct{ Method, Path string }\n\nfunc RegisterGRPC(a, b string, c, d any) {}\n")

	got := routeMap(mustAnalyze(t, root, "routers/users.go"))
	for _, route := range []string{"GET /api/users", "POST /api/users"} { // group 前綴：比對結尾；method 大小寫不拘
		if g := got[route]; !g.Direct || g.Reason != "registered in this file" {
			t.Errorf("%s = %+v", route, g)
		}
	}
	// 同套件的其他檔案註冊的路由：間接
	if g := got["GET /health"]; g.Route == "" || g.Direct || !strings.Contains(g.Reason, "routers") {
		t.Errorf("GET /health = %+v", g)
	}
	if _, ok := got["GET /unrelated"]; ok {
		t.Error("GET /unrelated should not be affected")
	}
	if _, ok := got["POST /api/admin-users"]; ok { // 同 method，但 /users 不是 /admin-users 的路徑區段
		t.Error("suffix match must respect path segments")
	}
	// manifest 裡 GET /api/users 排在前面，但它對 setup.go 是間接的：直接受影響的要排最前面
	setup := mustAnalyze(t, root, "routers/setup.go")
	if len(setup.Routes) == 0 || setup.Routes[0].Route != "GET /health" || !setup.Routes[0].Direct {
		t.Errorf("direct routes should come first: %+v", setup.Routes)
	}
	if g := routeMap(mustAnalyze(t, root, "rpc/schema.go"))["grpc Svc/Get"]; !g.Direct {
		t.Errorf("grpc registration = %+v", g)
	}
	// 註冊路由的套件依賴 pkg/schema：改 schema 時這些路由也受影響
	if g := routeMap(mustAnalyze(t, root, "pkg/schema/s.go"))["POST /api/users"]; g.Route == "" || g.Direct {
		t.Errorf("through dependency = %+v", g)
	}
}

func TestRoutesUnknownWithoutManifest(t *testing.T) {
	r := mustAnalyze(t, moduleProject(t), "pkg/core/core.go")
	if r.RoutesKnown || !strings.Contains(FormatReport(r), "Routes: unknown") {
		t.Errorf("should say routes are unknown, not none:\n%s", FormatReport(r))
	}
}

func TestTestsAndCommand(t *testing.T) {
	r := mustAnalyze(t, moduleProject(t), "pkg/core/core.go")
	if want := "go test ./pkg/core/ ./pkg/svc/ ./tool/"; r.TestCommand != want {
		t.Errorf("TestCommand = %q, want %q", r.TestCommand, want)
	}
	if strings.Join(r.UntestedPackages, ",") != "app" {
		t.Errorf("UntestedPackages = %v", r.UntestedPackages)
	}
	for _, tt := range r.Tests {
		if strings.Contains(tt.File, `\`) {
			t.Errorf("test path should use forward slashes: %q", tt.File)
		}
	}
}

func TestRootPackageTestCommand(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "p.go", "package p\n")
	writeFile(t, root, "p_test.go", "package p\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) {}\n")
	if r := mustAnalyze(t, root, "p.go"); r.TestCommand != "go test ." {
		t.Errorf("TestCommand = %q", r.TestCommand)
	}
}

func TestTestFileTarget(t *testing.T) {
	r := mustAnalyze(t, moduleProject(t), "pkg/core/core_test.go")
	if !r.IsTestFile || r.Risk != RiskLow || r.TestCommand != "go test ./pkg/core/" {
		t.Errorf("report = %+v", r)
	}
	if len(r.Dependents) != 0 || len(r.API) != 0 {
		t.Errorf("a test file should not report dependents or API: %+v", r)
	}
}

func TestBunModelSuggestsMigrate(t *testing.T) {
	root := moduleProject(t)
	writeFile(t, root, "pkg/core/model.go", `package core

import "github.com/uptrace/bun"

type User struct {
	bun.BaseModel
}
`)
	r := mustAnalyze(t, root, "pkg/core/model.go")
	if !strings.Contains(strings.Join(r.NextSteps, "\n"), "hyp migrate diff") {
		t.Errorf("NextSteps = %v", r.NextSteps)
	}
	if strings.Contains(strings.Join(mustAnalyze(t, root, "pkg/core/core.go").NextSteps, "\n"), "migrate") {
		t.Error("files without models should not suggest migrate")
	}
}

// 檔案改到一半（語法錯誤）也要能分析依賴，只是少了 API
func TestUnparsableTargetStillReports(t *testing.T) {
	root := moduleProject(t)
	writeFile(t, root, "pkg/core/broken.go", "package core\n\nfunc Broken( {\n")
	r := mustAnalyze(t, root, "pkg/core/broken.go")
	if len(r.Warnings) == 0 || !strings.Contains(FormatReport(r), "Warning:") {
		t.Errorf("should warn: %+v", r)
	}
	if !strings.Contains(dependentPaths(r), "pkg/svc") {
		t.Errorf("dependents should still be listed: %q", dependentPaths(r))
	}
}

func TestDotImportIsReported(t *testing.T) {
	root := moduleProject(t)
	writeFile(t, root, "pkg/dot/dot.go", "package dot\n\nimport . \"example.com/project/pkg/core\"\n\nvar _ = Version\n")
	r := mustAnalyze(t, root, "pkg/core/core.go")
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "pkg/dot dot-imports") {
		t.Errorf("Warnings = %v", r.Warnings)
	}
}

func TestAssessRisk(t *testing.T) {
	deps := func(n int, testOnly bool) []DependentFile {
		out := make([]DependentFile, n)
		for i := range out {
			out[i].TestOnly = testOnly
		}
		return out
	}
	own := []TestImpact{{File: "p/*_test.go", TestCount: 1}}
	withOutside := func(n int) []TestImpact {
		return append(append([]TestImpact(nil), own...), TestImpact{File: "q/*_test.go", TestCount: n})
	}
	for name, tt := range map[string]struct {
		r    ImpactReport
		want string
	}{
		"no dependents":                  {ImpactReport{Tests: own}, RiskLow},
		"no dependents no tests":         {ImpactReport{}, RiskLow},
		"one dependent":                  {ImpactReport{Dependents: deps(1, false), Tests: own}, RiskLow},
		"tests only importers":           {ImpactReport{Dependents: deps(9, true), Tests: own}, RiskLow},
		"two direct":                     {ImpactReport{Dependents: deps(2, false), Tests: own}, RiskMedium},
		"three total":                    {ImpactReport{Dependents: deps(1, false), Transitive: make([]TransitiveDep, 2), Tests: own}, RiskMedium},
		"five direct":                    {ImpactReport{Dependents: deps(5, false), Tests: own}, RiskHigh},
		"six total":                      {ImpactReport{Dependents: deps(1, false), Transitive: make([]TransitiveDep, 5), Tests: own}, RiskHigh},
		"20 outside tests":               {ImpactReport{Dependents: deps(1, false), Tests: withOutside(20)}, RiskMedium},
		"many outside tests stay medium": {ImpactReport{Dependents: deps(1, false), Tests: withOutside(500)}, RiskMedium},
		"own tests don't count":          {ImpactReport{Tests: []TestImpact{{File: "p/*_test.go", TestCount: 500}}}, RiskLow},
		"direct route":                   {ImpactReport{Tests: own, Routes: []RouteImpact{{Direct: true}}}, RiskMedium},
		"indirect route":                 {ImpactReport{Tests: own, Routes: []RouteImpact{{}}}, RiskLow},
		"no own tests raises":            {ImpactReport{Dependents: deps(1, false)}, RiskMedium},
		"no own tests stops at medium":   {ImpactReport{Dependents: deps(1, false), Tests: []TestImpact{{File: "q/*_test.go", TestCount: 60}}}, RiskMedium},
		"no own tests caps":              {ImpactReport{Dependents: deps(5, false)}, RiskHigh},
	} {
		t.Run(name, func(t *testing.T) {
			r := tt.r
			r.Package = "p"
			risk, reasons := assessRisk(&r)
			if risk != tt.want {
				t.Errorf("risk = %s, want %s (%v)", risk, tt.want, reasons)
			}
			if len(reasons) == 0 {
				t.Error("every risk level needs a reason")
			}
		})
	}
}

func TestFormatJSONRoundTrip(t *testing.T) {
	r := mustAnalyze(t, routesProject(t), "app/models/user.go")
	out, err := FormatJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var back ImpactReport
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatal(err)
	}
	if back.Risk != r.Risk || len(back.Routes) != len(r.Routes) || back.Package != "app/models" || !back.RoutesKnown {
		t.Errorf("round trip = %+v", back)
	}
}

// 文字報告有上限（常被貼給 AI），JSON 保留完整內容
func TestFormatReportLimitsLongLists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, "core/core.go", "package core\n")
	for i := 0; i < reportListLimit+5; i++ {
		name := "dep" + string(rune('a'+i))
		writeFile(t, root, name+"/x.go", "package "+name+"\n\nimport _ \"example.com/project/core\"\n")
	}
	r := mustAnalyze(t, root, "core/core.go")
	if len(r.Dependents) != reportListLimit+5 {
		t.Fatalf("Dependents = %d", len(r.Dependents))
	}
	text := FormatReport(r)
	if !strings.Contains(text, "... and 5 more") || strings.Count(text, "→ dep") != reportListLimit {
		t.Errorf("text report not limited:\n%s", text)
	}
}
