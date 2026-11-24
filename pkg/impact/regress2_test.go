package impact

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shopProject：routers → controllers → services → models，另有同名的 admin/controllers、
// 不參與建置的 tools/gen.go 與 HTTP 客戶端 util
func shopProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, ".hyp/context.yaml", `routes:
  - method: GET
    path: /api/users
    handler_names: ["controllers.(*UserController).List"]
  - method: GET
    path: /api/users/:id
    handler_names: ["controllers.(*UserController).Get-fm"]
    output_type: models.UserResp
  - method: GET
    path: /api/orders/:id
    handler_names: ["controllers.(*OrderController).Get"]
    output_type: "api.Page[models.Order]"
  - method: GET
    path: /admin/stats
    handler_names: ["controllers.Stats"]
    input_type: other.UserResp
  - method: GET
    path: /api/removed
    handler_names: ["controllers.Removed"]
  - method: POST
    path: /api/items
    handler_names: ["routers.Items.func1"]
`)
	writeFile(t, root, "app/models/user.go", "package models\n\ntype User struct{ ID int }\n\ntype UserResp struct{ ID int }\n\nfunc (u *User) Save() error { return validate(u) }\n")
	writeFile(t, root, "app/models/order.go", "package models\n\ntype Order struct{ ID int }\n\nfunc (o *Order) Save() error { return validate(o) }\n\nvar _ = internalState{n: limit}\n")
	writeFile(t, root, "app/models/helper.go", "package models\n\nfunc validate(v any) error { return nil }\n\ntype internalState struct{ n int }\n\nconst limit = 3\n\nfunc unused() {}\n")
	writeFile(t, root, "app/models/other.go", "package models\n\n// 區域變數與欄位同名：不是對 helper.go 的引用\nfunc local() int {\n\tvalidate := 1\n\ttype t struct{ limit int }\n\treturn validate + t{limit: 2}.limit\n}\n")
	writeFile(t, root, "app/models/helper_test.go", "package models\n\nimport \"testing\"\n\nfunc TestValidate(t *testing.T) { _ = validate(nil) }\n")
	writeFile(t, root, "app/services/user.go", "package services\n\nimport \"example.com/project/app/models\"\n\nfunc FindUser(id int) *models.User { return &models.User{ID: id} }\n")
	writeFile(t, root, "app/controllers/user.go", `package controllers

import (
	"example.com/project/app/models"
	"example.com/project/app/services"
)

type UserController struct{}

func (c *UserController) Get(ctx any)  { _ = services.FindUser(1); _ = models.UserResp{} }
func (c *UserController) List(ctx any) {}
`)
	writeFile(t, root, "app/controllers/order.go", "package controllers\n\ntype OrderController struct{}\n\nfunc (c *OrderController) Get(ctx any) {}\n")
	writeFile(t, root, "app/admin/controllers/stats.go", "package controllers\n\nfunc Stats(ctx any) {}\n")
	writeFile(t, root, "routers/router.go", "package routers\n\ntype Router interface {\n\tGroup(string) Router\n\tGET(string, any)\n\tPOST(string, any)\n}\n")
	writeFile(t, root, "routers/user.go", `package routers

import "example.com/project/app/controllers"

func Users(r Router) {
	c := &controllers.UserController{}
	api := r.Group("/api")
	g := api.Group("users")
	g.GET("", c.List)
	g.GET("/:id", c.Get)
}
`)
	writeFile(t, root, "routers/order.go", `package routers

import "example.com/project/app/controllers"

func Orders(r Router) {
	c := &controllers.OrderController{}
	r.Group("/api/orders").GET("/:id", c.Get)
}
`)
	writeFile(t, root, "routers/items.go", "package routers\n\nfunc Items(g Router) {\n\tg.POST(\"/items\", func(ctx any) {})\n}\n")
	writeFile(t, root, "util/client.go", "package util\n\ntype client struct{}\n\nfunc (client) GET(url string) {}\n\nfunc Ping() { client{}.GET(\"/:id\") }\n")
	writeFile(t, root, "tools/gen.go", "//go:build ignore\n\npackage main\n\nimport _ \"example.com/project/app/models\"\n\nfunc main() {}\n")
	writeFile(t, root, "tools/gen2.go", "//go:build ignore && tools\n// +build ignore,tools\n\npackage main\n\nimport _ \"example.com/project/app/models\"\n")
	writeFile(t, root, "plat/plat_linux.go", "//go:build linux && !windows\n\npackage plat\n\nimport _ \"example.com/project/app/models\"\n")
	// manifest 比所有原始碼新：預設不該有「過期」的警告
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(root, ".hyp", "context.yaml"), future, future)
	return root
}

// 先前路徑大小寫與磁碟上不同時（Windows、macOS 找得到檔案）回報「沒有任何依賴、LOW」
func TestPathCaseDoesNotHideDependents(t *testing.T) {
	root := shopProject(t)
	want := mustAnalyze(t, root, "app/models/user.go")
	if want.Risk == RiskNone || len(want.Dependents) == 0 {
		t.Fatalf("baseline: %+v", want)
	}
	if _, err := os.Stat(filepath.Join(root, "APP", "Models", "user.go")); err != nil {
		t.Skip("case-sensitive file system: a differently cased path does not exist")
	}
	got := mustAnalyze(t, root, "APP/Models/user.go")
	if got.Package != "app/models" || got.Risk != want.Risk || dependentPaths(got) != dependentPaths(want) || len(got.API) != len(want.API) {
		t.Errorf("differently cased path: package=%s risk=%s dependents=%q", got.Package, got.Risk, dependentPaths(got))
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), "analysed as app/models") {
		t.Errorf("warnings = %v", got.Warnings)
	}
}

func TestFoldPackage(t *testing.T) {
	p := &project{packages: map[string]*pkgInfo{"pkg/errors": {}, "pkg/Dup": {}, "pkg/dup": {}}}
	for in, want := range map[string]string{"PKG/Errors": "pkg/errors", "pkg/DUP": "", "pkg/none": ""} {
		if got := p.foldPackage(in); got != want {
			t.Errorf("foldPackage(%q) = %q, want %q", in, got, want)
		}
	}
}

// 找不到套件時回錯，不輸出任何級別；Go 會略過的目錄與 //go:build ignore 的檔案是 NONE 並說明原因
func TestTargetOutsideTheBuild(t *testing.T) {
	root := shopProject(t)
	writeFile(t, root, "testdata/fix/a.go", "package fix\n")
	writeFile(t, root, "_drafts/a.go", "package drafts\n")
	writeFile(t, root, "broken/a.go", "pakage broken\n") // package 子句壞掉：掃描時被跳過
	for file, reason := range map[string]string{
		"testdata/fix/a.go": "directories named testdata",
		"_drafts/a.go":      "directories named _drafts",
		"tools/gen.go":      "//go:build ignore",
	} {
		r := mustAnalyze(t, root, file)
		if r.Risk != RiskNone || len(r.RiskReasons) != 1 || !strings.Contains(r.RiskReasons[0], reason) {
			t.Errorf("%s: risk=%s reasons=%v", file, r.Risk, r.RiskReasons)
		}
	}
	if r, err := Analyze(filepath.Join(root, "broken", "a.go"), root); err == nil || !strings.Contains(err.Error(), "no Go package found") {
		t.Errorf("unscanned package: report=%+v err=%v", r, err)
	}
}

func TestBuildIgnoredFilesAreNotDependents(t *testing.T) {
	root := shopProject(t)
	got := dependentPaths(mustAnalyze(t, root, "app/models/user.go"))
	// plat 的檔案只在 linux 編譯：仍算依賴者（寧可多報）；tools 的兩個檔案都要 ignore tag
	if got != "app/controllers,app/services,plat" {
		t.Errorf("dependents = %q", got)
	}
}

// 這一輪 schema 的修正讓 manifest 的型別名稱帶套件名，先前的比對因此一條也比不到
func TestRoutesFromQualifiedTypeNames(t *testing.T) {
	root := shopProject(t)
	got := routeMap(mustAnalyze(t, root, "app/models/user.go"))
	if g := got["GET /api/users/:id"]; !g.Direct || g.Reason != "output type models.UserResp" {
		t.Errorf("GET /api/users/:id = %+v", g)
	}
	// other.UserResp 是別的套件的同名型別
	if g := got["GET /admin/stats"]; g.Direct {
		t.Errorf("a same-named type of another package matched: %+v", g)
	}
	// 泛型的型別參數
	if g := routeMap(mustAnalyze(t, root, "app/models/order.go"))["GET /api/orders/:id"]; !g.Direct || !strings.Contains(g.Reason, "api.Page[models.Order]") {
		t.Errorf("generic type argument = %+v", g)
	}

	d := &fileDecls{pkgName: "models", symbols: map[string]string{"User": kindType, "Limit": kindConst}}
	for name, want := range map[string]bool{
		"User": true, "models.User": true, "[]models.User": true, "map[string]*models.User": true,
		"api.Page[models.User]": true, "other.User": false, "models.Limit": false, "models.Missing": false, "": false,
	} {
		if got := d.declaresType(name); got != want {
			t.Errorf("declaresType(%q) = %v, want %v", name, got, want)
		}
	}
}

// 先前只比對路徑結尾：routers/order.go 的 GET("/:id") 把 GET /api/users/:id 也認成自己註冊的
func TestRouteRegistrationUsesGroupPrefixAndHandler(t *testing.T) {
	root := shopProject(t)

	order := routeMap(mustAnalyze(t, root, "routers/order.go"))
	if g := order["GET /api/orders/:id"]; !g.Direct || g.Reason != "registered in this file" {
		t.Errorf("orders route = %+v", g)
	}
	if g := order["GET /api/users/:id"]; g.Direct {
		t.Errorf("users route claimed by routers/order.go: %+v", g)
	}

	// 兩層 Group（r.Group("/api") → api.Group("users")）接起來
	user := routeMap(mustAnalyze(t, root, "routers/user.go"))
	for _, route := range []string{"GET /api/users", "GET /api/users/:id"} {
		if g := user[route]; !g.Direct || g.Reason != "registered in this file" {
			t.Errorf("%s = %+v", route, g)
		}
	}
	if g := user["GET /api/orders/:id"]; g.Direct {
		t.Errorf("orders route claimed by routers/user.go: %+v", g)
	}

	// 接收者的前綴追不到、handler 是閉包：只有結尾相同，標為「可能」而不算直接…
	// 但這條路由的 handler 就是這個檔案的閉包，所以仍是直接（依 handler）
	if g := routeMap(mustAnalyze(t, root, "routers/items.go"))["POST /api/items"]; !g.Direct || !strings.Contains(g.Reason, "handler") {
		t.Errorf("closure handler = %+v", g)
	}

	// 不是路由：HTTP 客戶端的 client.GET("/:id")（沒有 handler 參數）
	if r := mustAnalyze(t, root, "util/client.go"); len(r.Routes) != 0 {
		t.Errorf("an HTTP client call was treated as a route registration: %+v", r.Routes)
	}
}

func TestMatchRefs(t *testing.T) {
	handlers := []string{"controllers.(*UserController).Get-fm"}
	for name, tt := range map[string]struct {
		ref  routeRef
		want int
	}{
		"exact":                    {routeRef{method: "GET", path: "/api/users/:id"}, matchSure},
		"suffix with group prefix": {routeRef{method: "GET", path: "/users/:id", grouped: true}, matchSure},
		// 只有方法名稱相同不算確認：每個 controller 都有 Get。
		// 歷史 bug：routers/order.go 的 g.GET("/:id", c.Get) 認領了 GET /api/users/:id
		"suffix, same method name only":  {routeRef{method: "GET", path: "/:id", handler: "Get", qual: "c"}, matchMaybe},
		"suffix only":                    {routeRef{method: "GET", path: "/:id"}, matchMaybe},
		"another handler":                {routeRef{method: "GET", path: "/:id", handler: "Show"}, matchNone},
		"exact path but another handler": {routeRef{method: "GET", path: "/api/users/:id", handler: "Show"}, matchNone},
		"another method":                 {routeRef{method: "POST", path: "/api/users/:id"}, matchNone},
		"not a path segment":             {routeRef{method: "GET", path: "sers/:id", grouped: true}, matchNone},
		"group prefix of another route":  {routeRef{method: "GET", path: "/api/orders/:id", grouped: true}, matchNone},
	} {
		if got := matchRefs([]routeRef{tt.ref}, "GET", "/api/users/:id", "", handlers); got != tt.want {
			t.Errorf("%s: matchRefs = %d, want %d", name, got, tt.want)
		}
	}
	// 套件層級的函式連套件名稱都相同才算確認
	pkgFunc := []string{"controllers.GetUser"}
	for name, tt := range map[string]struct {
		ref  routeRef
		want int
	}{
		"same package and name":      {routeRef{method: "GET", path: "/:id", handler: "GetUser", qual: "controllers"}, matchSure},
		"same name, another package": {routeRef{method: "GET", path: "/:id", handler: "GetUser", qual: "admin"}, matchMaybe},
	} {
		if got := matchRefs([]routeRef{tt.ref}, "GET", "/api/users/:id", "", pkgFunc); got != tt.want {
			t.Errorf("%s: matchRefs = %d, want %d", name, got, tt.want)
		}
	}
	// 「可能」的結果不蓋過後面確定的
	refs := []routeRef{{method: "GET", path: "/:id"}, {method: "GET", path: "/api/users/:id"}}
	if got := matchRefs(refs, "GET", "/api/users/:id", "", nil); got != matchSure {
		t.Errorf("sure after maybe = %d", got)
	}
	if got := matchRefs([]routeRef{{command: "Svc/Get"}}, "", "", "Svc/Get", nil); got != matchSure {
		t.Errorf("command = %d", got)
	}
}

// 只有路徑結尾相同、沒有其他佐證：列出但不算直接
func TestPossibleRegistrationIsNotDirect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", testModule)
	writeFile(t, root, ".hyp/context.yaml", "routes:\n  - method: POST\n    path: /api/items\n    handler_names: [elsewhere.Create]\n")
	writeFile(t, root, "routers/items.go", "package routers\n\nfunc Items(g interface{ POST(string, any) }, h any) {\n\tg.POST(\"/items\", h)\n}\n")
	r := mustAnalyze(t, root, "routers/items.go")
	if len(r.Routes) != 1 || r.Routes[0].Direct || !strings.HasPrefix(r.Routes[0].Reason, "possibly registered in this file") {
		t.Fatalf("routes = %+v", r.Routes)
	}
	if r.Risk != RiskNone {
		t.Errorf("a possible registration should not raise the risk: %s", r.Risk)
	}
	if !strings.Contains(FormatReport(r), "(possibly registered in this file") {
		t.Errorf("report does not explain the route:\n%s", FormatReport(r))
	}
}

// 先前同名的套件互相把對方的路由全部算成受影響，handler 已刪除的路由也照列
func TestHandlersMustBeDeclaredInAffectedPackage(t *testing.T) {
	root := shopProject(t)
	r := mustAnalyze(t, root, "app/admin/controllers/stats.go")
	if len(r.Routes) != 1 || r.Routes[0].Route != "GET /admin/stats" || !r.Routes[0].Direct {
		t.Errorf("routes = %+v", r.Routes)
	}
	// models → controllers：controllers 的路由間接受影響，但 handler 不存在的 /api/removed 不列
	got := routeMap(mustAnalyze(t, root, "app/models/helper.go"))
	for _, route := range []string{"GET /api/users", "GET /api/users/:id", "GET /api/orders/:id"} {
		if g := got[route]; g.Route == "" || g.Direct {
			t.Errorf("%s = %+v, want indirect", route, g)
		}
	}
	for _, route := range []string{"GET /api/removed", "GET /admin/stats"} {
		if _, ok := got[route]; ok {
			t.Errorf("%s should not be affected", route)
		}
	}
}

func TestHandlerFunc(t *testing.T) {
	for in, want := range map[string]string{
		"controllers.List":                     "controllers.List",
		"controllers.(*UserController).Get-fm": "controllers.(*UserController).Get",
		"routers.Setup.func1":                  "routers.Setup",
		"routers.Setup.func1.2":                "routers.Setup",
		"routers.Setup.func2.func1":            "routers.Setup",
		"pkg.Generic[...]":                     "pkg.Generic",
		"pkg.Generic[...].func1":               "pkg.Generic",
	} {
		if got := handlerFunc(in); got != want {
			t.Errorf("handlerFunc(%q) = %q, want %q", in, got, want)
		}
	}
}

// 先前原始碼改過之後 manifest 裡仍是舊的路由，報告照列而沒有提示
func TestStaleManifestIsReported(t *testing.T) {
	root := shopProject(t)
	if r := mustAnalyze(t, root, "app/models/user.go"); len(r.Warnings) != 0 {
		t.Fatalf("fresh manifest: warnings = %v", r.Warnings)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(root, ".hyp", "context.yaml"), old, old)
	newer := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(root, "app", "controllers", "order.go"), newer, newer)
	r := mustAnalyze(t, root, "app/models/user.go")
	w := strings.Join(r.Warnings, "\n")
	if !strings.Contains(w, "older than app/controllers/order.go") || !strings.Contains(w, "hyp context") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

// 先前報告只看其他套件：未匯出的函式被同套件的其他檔案使用，報告裡什麼都沒有
func TestSamePackageUses(t *testing.T) {
	root := shopProject(t)
	r := mustAnalyze(t, root, "app/models/helper.go")
	var got []string
	for _, u := range r.SamePackage {
		got = append(got, u.File+":"+strings.Join(u.Uses, "+"))
	}
	// other.go 的區域變數 validate 與欄位 limit 不算；測試檔也列出
	if want := "helper_test.go:validate,order.go:internalState+limit+validate,user.go:validate"; strings.Join(got, ",") != want {
		t.Errorf("same package uses = %v\nwant %s", got, want)
	}
	out := FormatReport(r)
	if !strings.Contains(out, "Used by other files in this package (3):") || !strings.Contains(out, "→ order.go  uses: internalState, limit, validate") {
		t.Errorf("report:\n%s", out)
	}
	if r := mustAnalyze(t, root, "app/models/other.go"); len(r.SamePackage) != 0 {
		t.Errorf("nothing uses other.go: %+v", r.SamePackage)
	}
}

// 間接依賴的套件，測試卻直接 import 目標：先前 API 區段有列，依賴清單看不出來
func TestTransitiveDependentWhoseTestsImportDirectly(t *testing.T) {
	root := shopProject(t)
	writeFile(t, root, "routers/routers_test.go", "package routers\n\nimport (\n\t\"testing\"\n\n\t\"example.com/project/app/models\"\n)\n\nfunc TestX(t *testing.T) { _ = models.User{} }\n")
	future := time.Now().Add(2 * time.Hour)
	os.Chtimes(filepath.Join(root, ".hyp", "context.yaml"), future, future)
	r := mustAnalyze(t, root, "app/models/user.go")
	var routers *TransitiveDep
	for i := range r.Transitive {
		if r.Transitive[i].Path == "routers" {
			routers = &r.Transitive[i]
		} else if r.Transitive[i].TestsImport {
			t.Errorf("%s does not import it from tests", r.Transitive[i].Path)
		}
	}
	if routers == nil || !routers.TestsImport {
		t.Fatalf("transitive = %+v", r.Transitive)
	}
	if !strings.Contains(FormatReport(r), "→ routers (via app/controllers; its tests also import this package directly)") {
		t.Errorf("report:\n%s", FormatReport(r))
	}
}

// 四個級別：NONE 是「這個檔案以外沒有東西受影響」
func TestRiskLevels(t *testing.T) {
	root := shopProject(t)
	writeFile(t, root, "cmd/app/main.go", "package main\n\nimport _ \"example.com/project/routers\"\n\nfunc main() {}\n")
	writeFile(t, root, "app/services/user_test.go", "package services\n\nimport \"testing\"\n\nfunc TestFind(t *testing.T) {}\n")
	future := time.Now().Add(2 * time.Hour)
	os.Chtimes(filepath.Join(root, ".hyp", "context.yaml"), future, future)

	main := mustAnalyze(t, root, "cmd/app/main.go")
	reasons := strings.Join(main.RiskReasons, "\n")
	if main.Risk != RiskNone || !strings.Contains(reasons, "package main") || !strings.Contains(reasons, "not that the change is safe") {
		t.Errorf("main.go: %s %v", main.Risk, main.RiskReasons)
	}
	if r := mustAnalyze(t, root, "util/client.go"); r.Risk != RiskNone || strings.Contains(strings.Join(r.RiskReasons, "\n"), "package main") {
		t.Errorf("leaf package: %s %v", r.Risk, r.RiskReasons)
	}
	// services 有自己的測試、只有 controllers 一個直接依賴者（routers、cmd/app 間接）：3 個 → MEDIUM
	if r := mustAnalyze(t, root, "app/services/user.go"); r.Risk != RiskMedium {
		t.Errorf("services: %s %v", r.Risk, r.RiskReasons)
	}
	// 只有 1 個依賴者而且有自己的測試：LOW
	writeFile(t, root, "leaf/leaf.go", "package leaf\n\nfunc F() {}\n")
	writeFile(t, root, "leaf/leaf_test.go", "package leaf\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {}\n")
	writeFile(t, root, "user/user.go", "package user\n\nimport \"example.com/project/leaf\"\n\nfunc G() { leaf.F() }\n")
	os.Chtimes(filepath.Join(root, ".hyp", "context.yaml"), future, future)
	if r := mustAnalyze(t, root, "leaf/leaf.go"); r.Risk != RiskLow {
		t.Errorf("one dependent: %s %v", r.Risk, r.RiskReasons)
	}
	_ = runtime.GOOS
}

// 巢狀與串接的 Group 前綴要整段接起來：只接最後一層時，/v1/users 與 /v2/users 分不出來
func TestScanRouteRefsJoinsGroupPrefixes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "r.go", `package routers

func Setup(r router, h handlers) {
	v1 := r.Group("/v1")
	users := v1.Group("users/")
	users.GET("/:id", h.Get)
	var admin = v1.Group("/admin")
	admin.Group("/audit").POST("", h.Audit)
	r.GET("/health", h.Health)
	users.Schema(schema.Route{Method: "put", Path: ":id"}).Handle(h.Update)
}
`)
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "r.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ref := range scanRouteRefs(f) {
		got = append(got, fmt.Sprintf("%s %s grouped=%v handler=%s", ref.method, ref.path, ref.grouped, ref.handler))
	}
	want := []string{
		"GET /v1/users/:id grouped=true handler=Get",
		"POST /v1/admin/audit grouped=true handler=Audit",
		"GET /health grouped=false handler=Health",
		"PUT /v1/users/:id grouped=true handler=Update",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
