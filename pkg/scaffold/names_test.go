package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 名稱直接當檔名：結尾是 _test 或 _<GOOS> / _<GOARCH> 時 Go 不會編譯那個檔案。
// 歷史 bug：hyp generate model page_js 產生 page_js.go（只在 GOOS=js 編譯），其他生成檔引用的型別找不到
func TestValidateNameRejectsBuildSuffixes(t *testing.T) {
	for name, ok := range map[string]bool{
		"order": true, "order_item": true, "linux": true, "js": true, "tester": true, "latest": true, "arm_wrestling": true,
		"page_js": false, "log_linux": false, "x_arm64": false, "load_test": false, "Load_Test": false, "a_windows": false,
	} {
		if err := ValidateName(name); (err == nil) != ok {
			t.Errorf("ValidateName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
}

// 歷史 bug：users → userses、data → datas
func TestPluralize(t *testing.T) {
	for in, want := range map[string]string{
		"user": "users", "users": "users", "category": "categories", "box": "boxes", "status": "statuses",
		"address": "addresses", "analysis": "analysises", "data": "data", "order_items": "order_items",
		"order_item": "order_items", "user_data": "user_data", "bus": "buses", "news": "news", "key": "keys",
	} {
		if got := pluralize(in); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", in, got, want)
		}
	}
}

// resource 在暫存專案裡生成一組資源（model + service + controller + router）
func resource(t *testing.T, root, name string) error {
	t.Helper()
	for _, err := range []error{
		GenerateModel(filepath.Join(root, "app", "models"), name),
		GenerateService(filepath.Join(root, "app", "services"), name, "demo"),
		GenerateController(filepath.Join(root, "app", "controllers"), name, "demo"),
		GenerateRouter(filepath.Join(root, "routers"), name, "demo"),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

func newProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// 生成前比對同套件既有的識別字。
// 歷史 bug：已有 user 時生成 user_list，兩邊都宣告 UserListResp，整個套件編不過
func TestGenerateRejectsIdentifierCollisions(t *testing.T) {
	root := newProject(t)
	if err := resource(t, root, "user"); err != nil {
		t.Fatal(err)
	}
	models := filepath.Join(root, "app", "models")
	for _, name := range []string{"user_list", "user_resp", "create_user_req", "update_user_req", "user_list_resp"} {
		err := GenerateModel(models, name)
		if err == nil || !strings.Contains(err.Error(), "already declares") {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(models, name+".go")); statErr == nil {
			t.Errorf("%s: file was written despite the collision", name)
		}
	}
	// 順序反過來也要擋：先有 order_list，再生成 order
	if err := GenerateModel(models, "order_list"); err != nil {
		t.Fatal(err)
	}
	if err := GenerateModel(models, "order"); err == nil {
		t.Error("order should collide with the existing order_list (OrderListResp)")
	}
	// 不相干的名稱不受影響；手寫的型別也算
	if err := GenerateModel(models, "price_list"); err != nil {
		t.Errorf("price_list should be fine: %v", err)
	}
	os.WriteFile(filepath.Join(models, "hand.go"), []byte("package models\n\ntype Invoice struct{}\n"), 0o644)
	if err := GenerateModel(models, "invoice"); err == nil {
		t.Error("a hand-written type of the same name should be detected")
	}
	// 測試檔與解析不了的檔案不影響生成
	os.WriteFile(filepath.Join(models, "x_test.go"), []byte("package models\n\ntype Payment struct{}\n"), 0o644)
	os.WriteFile(filepath.Join(models, "broken.go"), []byte("package models\n\nfunc (\n"), 0o644)
	if err := GenerateModel(models, "payment"); err != nil {
		t.Errorf("test files and unparsable files should be ignored: %v", err)
	}
}

// 生成前比對專案裡既有的錯誤碼：重複定義編譯會過，但行程一啟動就 panic。
// 歷史 bug：controller svc_user 與 service user 都定義 E_svc_user_001
func TestGenerateRejectsErrorCodeCollisions(t *testing.T) {
	root := newProject(t)
	// 專案別處（不同套件）已經用掉這個碼
	other := filepath.Join(root, "internal", "billing")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "errors.go"),
		[]byte("package billing\n\nimport \"github.com/maoxiaoyue/hypgo/pkg/errors\"\n\nvar ErrX = errors.Define(\"E_order_001\", 404, \"x\", \"billing\")\n"), 0o644)

	services := filepath.Join(root, "app", "services")
	err := GenerateService(services, "order", "demo")
	if err == nil || !strings.Contains(err.Error(), "E_order_001") || !strings.Contains(err.Error(), "errors.go") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(services, "order_service.go")); statErr == nil {
		t.Error("service was written despite the duplicate error code")
	}
	if err := GenerateService(services, "invoice", "demo"); err != nil {
		t.Errorf("an unrelated resource should be fine: %v", err)
	}
	// vendor 與隱藏目錄不掃
	os.MkdirAll(filepath.Join(root, "vendor", "x"), 0o755)
	os.WriteFile(filepath.Join(root, "vendor", "x", "e.go"), []byte("package x\n\nvar _ = errors.Define(\"E_payment_001\", 404, \"x\", \"x\")\n"), 0o644)
	if err := GenerateService(services, "payment", "demo"); err != nil {
		t.Errorf("vendor should not be scanned: %v", err)
	}
}

// 資源的錯誤碼集中在 service：svc_ 開頭的資源不再與另一個資源的 service 撞碼
func TestSvcPrefixedResourceDoesNotClash(t *testing.T) {
	root := newProject(t)
	if err := resource(t, root, "user"); err != nil {
		t.Fatal(err)
	}
	if err := resource(t, root, "svc_user"); err != nil {
		t.Fatalf("svc_user should not clash with user: %v", err)
	}
}

// 未實作的端點回 501，不回假資料；controller 委派給 service，型別來自 models。
// 歷史 bug：controller 直接回傳寫死的 id: 1（任何 id 都是 200），service 用 map[string]interface{} 而且沒人呼叫
func TestGeneratedResourceShape(t *testing.T) {
	root := newProject(t)
	if err := resource(t, root, "order_item"); err != nil {
		t.Fatal(err)
	}
	read := func(rel ...string) string {
		data, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	ctrl := read("app", "controllers", "order_item_controller.go")
	for _, want := range []string{
		"Service *services.OrderItemService", "services.ErrOrderItemNotImplemented", "strconv.ParseInt",
		"svc.GetByID(c.Request.Context(), id)", "svc.Create(c.Request.Context(), req)",
	} {
		if !strings.Contains(ctrl, want) {
			t.Errorf("controller missing %q", want)
		}
	}
	for _, bad := range []string{"ID: 1", "ID:   1", "errors.Define"} {
		if strings.Contains(ctrl, bad) {
			t.Errorf("controller should not contain %q", bad)
		}
	}
	svc := read("app", "services", "order_item_service.go")
	for _, want := range []string{
		`errors.Define("E_order_item_003", 501`, "(ctx context.Context, req models.CreateOrderItemReq) (*models.OrderItemResp, error)",
		"([]models.OrderItemResp, error)", "return nil, ErrOrderItemNotImplemented", `"demo/app/models"`,
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("service missing %q", want)
		}
	}
	if strings.Contains(svc, "interface{}") || strings.Contains(svc, "E_svc_") {
		t.Error("service should be typed and own the resource's error codes")
	}
	// 路由用複數，表名一致
	rt := read("routers", "order_item.go")
	if !strings.Contains(rt, `"/api/order_items"`) || !strings.Contains(rt, `"/api/order_items/:id"`) || strings.Contains(rt, `"/api/order_item"`) {
		t.Errorf("routes should be plural:\n%.600s", rt)
	}
	if !strings.Contains(read("app", "models", "order_item.go"), "table:order_items") {
		t.Error("table name should match the route")
	}
}

// 沒給 module 路徑：先前預設成 myapp，生成的 import 一定是錯的
func TestGeneratorsRequireModule(t *testing.T) {
	dir := t.TempDir()
	for name, err := range map[string]error{
		"controller": GenerateController(dir, "a", ""), "router": GenerateRouter(dir, "b", ""),
		"setup": GenerateRouterSetup(dir, "c", ""), "service": GenerateService(dir, "d", ""), "proto": GenerateProto(dir, "e", ""),
	} {
		if err == nil {
			t.Errorf("%s: an empty module name should be an error", name)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files were written without a module: %v", entries)
	}
}

func TestCommandTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCommand(dir, "export_data"); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join(dir, "export_data.go"))
	// 變數用 lowerCamel（先前 export_dataCmd）；於 init 登錄 Schema（先前 hyp context 看不到新命令）
	for _, want := range []string{"var exportDataCmd = &cobra.Command{", `Use:   "export_data"`, `schema.RegisterCLI("export_data"`, "func runExportData("} {
		if !strings.Contains(string(src), want) {
			t.Errorf("command missing %q:\n%s", want, src)
		}
	}
	// 會蓋掉 cobra 內建命令的名稱
	for _, name := range []string{"help", "Help", "completion"} {
		if err := GenerateCommand(dir, name); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestGRPCServiceNameAndMakefile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if err := GenerateGRPCProject(dir, "user-service", "user-service"); err != nil {
		t.Fatal(err)
	}
	proto, _ := os.ReadFile(filepath.Join(dir, "app", "proto", "user_servicepb", "user_service.proto"))
	// 先前名稱以 service 結尾時服務叫 UserServiceService
	if !strings.Contains(string(proto), "service UserService {") || strings.Contains(string(proto), "ServiceService") {
		t.Errorf("service name:\n%.400s", proto)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), "pb.RegisterUserServiceServer(") {
		t.Error("main.go should register UserService")
	}
	// make proto 要編譯 app/proto 下全部的 .proto：先前只有專案最初那一個，hyp generate proto 加的不會被編譯
	mk, _ := os.ReadFile(filepath.Join(dir, "Makefile"))
	if !strings.Contains(string(mk), "app/proto/*/*.proto") {
		t.Errorf("Makefile should compile every proto:\n%s", mk)
	}
	if err := GenerateGRPCProject(filepath.Join(t.TempDir(), "q"), "order", "order"); err != nil {
		t.Fatal(err)
	}
}
