package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"User", false},
		{"user", false},
		{"UserProfile", false},
		{"user_profile", false},
		{"user123", false},
		{"", true},
		{"123user", true},
		{"user-name", true},
		{"user.name", true},
		{"../../../etc/passwd", true},
		{"user/name", true},
		{strings.Repeat("a", 65), true},
	}

	for _, tt := range tests {
		err := validateName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestGenerateController(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateController(dir, "Product", "myapp"); err != nil {
		t.Fatalf("GenerateController failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "product_controller.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "hypcontext") {
		t.Error("should use hypcontext")
	}
	if !strings.Contains(s, "errors.Define") {
		t.Error("should use errors.Define")
	}
	if !strings.Contains(s, "ProductController") {
		t.Error("should contain ProductController")
	}
	// Controller 不應有 Schema 路由（已移至 routers/）
	if strings.Contains(s, "schema.Route") {
		t.Error("controller should NOT contain schema.Route (moved to routers/)")
	}
	// 但應引用 models
	if !strings.Contains(s, `"myapp/app/models"`) {
		t.Error("should import models package")
	}
}

func TestGenerateRouter(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateRouter(dir, "Product", "myapp"); err != nil {
		t.Fatalf("GenerateRouter failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "product.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "schema.Route") {
		t.Error("router should contain schema.Route")
	}
	if !strings.Contains(s, "RegisterProductRoutes") {
		t.Error("should contain RegisterProductRoutes")
	}
	if !strings.Contains(s, "models.CreateProductReq{}") {
		t.Error("should reference Input type")
	}
	if !strings.Contains(s, "models.ProductResp{}") {
		t.Error("should reference Output type")
	}
	if !strings.Contains(s, "models.ProductListResp{}") {
		t.Error("should reference List Output type")
	}
	if !strings.Contains(s, `"myapp/app/controllers"`) {
		t.Error("should import controllers package")
	}
	if !strings.Contains(s, `"myapp/app/models"`) {
		t.Error("should import models package")
	}
}

func TestGenerateRouterSetup(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateRouterSetup(dir, "Product", "myapp"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "router.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "func Setup") {
		t.Error("should contain Setup function")
	}
	if !strings.Contains(s, "middleware.DefaultMiddleware") {
		t.Error("should reference default middleware")
	}
}

func TestGenerateMiddleware(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateMiddleware(dir); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "middleware.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "APIMiddleware") {
		t.Error("should contain APIMiddleware")
	}
	if !strings.Contains(s, "WebMiddleware") {
		t.Error("should contain WebMiddleware")
	}
}

func TestGenerateModel(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateModel(dir, "Order"); err != nil {
		t.Fatalf("GenerateModel failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "order.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "bun.BaseModel") {
		t.Error("should use bun.BaseModel")
	}
	if !strings.Contains(s, `table:orders`) {
		t.Error("should have table:orders")
	}
	if !strings.Contains(s, "CreateOrderReq") {
		t.Error("should define CreateOrderReq")
	}
	if !strings.Contains(s, "UpdateOrderReq") {
		t.Error("should define UpdateOrderReq")
	}
	if !strings.Contains(s, "OrderResp") {
		t.Error("should define OrderResp")
	}
	if !strings.Contains(s, "OrderListResp") {
		t.Error("should define OrderListResp")
	}
}

func TestGenerateService(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateService(dir, "Payment"); err != nil {
		t.Fatalf("GenerateService failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "payment_service.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)
	if !strings.Contains(s, "errors.Define") {
		t.Error("should use errors.Define")
	}
	if !strings.Contains(s, "PaymentService") {
		t.Error("should contain PaymentService")
	}
}

func TestGenerateNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateController(dir, "User", "myapp"); err != nil {
		t.Fatal(err)
	}
	err := GenerateController(dir, "User", "myapp")
	if err == nil {
		t.Error("should fail when file exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestGenerateInvalidName(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateController(dir, "../hack", ""); err == nil {
		t.Error("should reject path traversal")
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"user", "User"},
		{"User", "User"},
		{"", ""},
		{"a", "A"},
	}
	for _, tt := range tests {
		if got := capitalize(tt.in); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
