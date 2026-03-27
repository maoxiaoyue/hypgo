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
		{"", true},                    // empty
		{"123user", true},             // starts with digit
		{"user-name", true},           // hyphen
		{"user.name", true},           // dot
		{"../../../etc/passwd", true}, // path traversal
		{"user/name", true},           // slash
		{strings.Repeat("a", 65), true}, // too long
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
	if err := GenerateController(dir, "Product"); err != nil {
		t.Fatalf("GenerateController failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "product_controller.go"))
	if err != nil {
		t.Fatal(err)
	}

	s := string(content)

	// 驗證使用 HypGo 原生 API
	if !strings.Contains(s, "hypcontext") {
		t.Error("should use hypcontext")
	}
	if !strings.Contains(s, "schema.Route") {
		t.Error("should use schema.Route")
	}
	if !strings.Contains(s, "errors.Define") {
		t.Error("should use errors.Define")
	}
	if !strings.Contains(s, "ProductController") {
		t.Error("should contain ProductController")
	}
	if !strings.Contains(s, "ErrProductNotFound") {
		t.Error("should define ErrProductNotFound")
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
	if !strings.Contains(s, "pk,autoincrement") {
		t.Error("should have pk,autoincrement")
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
	if !strings.Contains(s, "ErrSvcPaymentNotFound") {
		t.Error("should define ErrSvcPaymentNotFound")
	}
}

func TestGenerateNoOverwrite(t *testing.T) {
	dir := t.TempDir()

	if err := GenerateController(dir, "User"); err != nil {
		t.Fatal(err)
	}

	// 再次生成應該失敗
	err := GenerateController(dir, "User")
	if err == nil {
		t.Error("should fail when file exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestGenerateInvalidName(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateController(dir, "../hack"); err == nil {
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
