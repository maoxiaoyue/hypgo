// Package scaffold 提供智慧程式碼生成功能
// 生成的程式碼自動整合 Schema-first 路由、Error Catalog 和 Contract Testing
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// validName 只允許字母、數字、底線（防止目錄穿越和 code injection）
var validName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// GenerateController 生成 controller（只含 handler 邏輯，路由在 routers/ 中）
func GenerateController(dir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = "myapp"
	}
	data := templateData(name)
	data["ModuleName"] = moduleName
	return generateFile(dir, strings.ToLower(name)+"_controller.go", controllerTemplate, data)
}

// GenerateRouter 生成單一資源的 Schema-first 路由定義（routers/<name>.go）
func GenerateRouter(dir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = "myapp"
	}
	data := templateData(name)
	data["ModuleName"] = moduleName
	return generateFile(dir, strings.ToLower(name)+".go", routerTemplate, data)
}

// GenerateRouterSetup 生成 routers/router.go 總入口（只在首次執行）
func GenerateRouterSetup(dir, name, moduleName string) error {
	if moduleName == "" {
		moduleName = "myapp"
	}
	data := map[string]string{
		"Name":       capitalize(name),
		"ModuleName": moduleName,
	}
	return generateFile(dir, "router.go", routerSetupTemplate, data)
}

// GenerateMiddleware 生成 routers/middleware.go 中間件配置（只在首次執行）
func GenerateMiddleware(dir string) error {
	return generateFile(dir, "middleware.go", middlewareTemplate, nil)
}

// GenerateModel 生成使用 bun ORM 的 model（含 Request/Response struct）
func GenerateModel(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	data := templateData(name)
	data["TableName"] = strings.ToLower(name) + "s"
	data["Alias"] = strings.ToLower(name[:1])
	return generateFile(dir, strings.ToLower(name)+".go", modelTemplate, data)
}

// ============================================================
// CLI 專案生成
// ============================================================

// GenerateCLIProject 生成完整的 CLI 專案骨架
func GenerateCLIProject(baseDir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = name
	}

	data := templateData(name)
	data["ModuleName"] = moduleName

	// 建立目錄結構
	dirs := []string{
		filepath.Join(baseDir, "app", "commands"),
		filepath.Join(baseDir, "app", "models"),
		filepath.Join(baseDir, "app", "services"),
		filepath.Join(baseDir, "app", "config"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("scaffold: failed to create directory %s: %w", dir, err)
		}
	}

	// 生成檔案
	files := []struct {
		dir, filename, tmpl string
	}{
		{baseDir, "main.go", cliMainTemplate},
		{filepath.Join(baseDir, "app", "commands"), "root.go", cliRootTemplate},
		{filepath.Join(baseDir, "app", "config"), "config.yaml", cliConfigTemplate},
		{baseDir, "go.mod", cliGoModTemplate},
	}

	for _, f := range files {
		if err := generateFile(f.dir, f.filename, f.tmpl, data); err != nil {
			return err
		}
	}

	return nil
}

// GenerateCommand 生成 CLI 子命令（app/commands/<name>.go）
func GenerateCommand(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	return generateFile(dir, strings.ToLower(name)+".go", cliCommandTemplate, templateData(name))
}

// ============================================================
// Desktop 專案生成（Fyne）
// ============================================================

// GenerateDesktopProject 生成完整的 Desktop 專案骨架（Fyne）
func GenerateDesktopProject(baseDir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = name
	}

	data := templateData(name)
	data["ModuleName"] = moduleName

	// 建立目錄結構
	dirs := []string{
		filepath.Join(baseDir, "app", "views"),
		filepath.Join(baseDir, "app", "models"),
		filepath.Join(baseDir, "app", "services"),
		filepath.Join(baseDir, "app", "config"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("scaffold: failed to create directory %s: %w", dir, err)
		}
	}

	// 生成檔案
	files := []struct {
		dir, filename, tmpl string
	}{
		{baseDir, "main.go", desktopMainTemplate},
		{filepath.Join(baseDir, "app", "views"), "main_view.go", desktopViewTemplate},
		{filepath.Join(baseDir, "app", "config"), "config.yaml", desktopConfigTemplate},
		{baseDir, "go.mod", desktopGoModTemplate},
	}

	for _, f := range files {
		if err := generateFile(f.dir, f.filename, f.tmpl, data); err != nil {
			return err
		}
	}

	return nil
}

// GenerateView 生成 Desktop 自訂 view（app/views/<name>_view.go）
func GenerateView(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	return generateFile(dir, strings.ToLower(name)+"_view.go", desktopCustomViewTemplate, templateData(name))
}

// ============================================================
// gRPC 專案生成
// ============================================================

// GenerateGRPCProject 生成完整的 gRPC 微服務專案骨架
func GenerateGRPCProject(baseDir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = name
	}

	data := templateData(name)
	data["ModuleName"] = moduleName

	// 建立目錄結構
	dirs := []string{
		filepath.Join(baseDir, "app", "proto", strings.ToLower(name)+"pb"),
		filepath.Join(baseDir, "app", "rpc"),
		filepath.Join(baseDir, "app", "models"),
		filepath.Join(baseDir, "app", "services"),
		filepath.Join(baseDir, "app", "config"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("scaffold: failed to create directory %s: %w", dir, err)
		}
	}

	lowerName := strings.ToLower(name)

	// 生成檔案
	files := []struct {
		dir, filename, tmpl string
	}{
		{baseDir, "main.go", grpcMainTemplate},
		{filepath.Join(baseDir, "app", "proto", lowerName+"pb"), lowerName + ".proto", grpcProtoTemplate},
		{filepath.Join(baseDir, "app", "rpc"), lowerName + "_server.go", grpcServerTemplate},
		{filepath.Join(baseDir, "app", "config"), "config.yaml", grpcConfigTemplate},
		{baseDir, "go.mod", grpcGoModTemplate},
		{baseDir, "Makefile", grpcMakefileTemplate},
	}

	for _, f := range files {
		if err := generateFile(f.dir, f.filename, f.tmpl, data); err != nil {
			return err
		}
	}

	return nil
}

// GenerateProto 生成新的 .proto 定義 + gRPC server 實作
func GenerateProto(baseDir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = "myapp"
	}

	data := templateData(name)
	data["ModuleName"] = moduleName
	lowerName := strings.ToLower(name)

	protoDir := filepath.Join(baseDir, "app", "proto", lowerName+"pb")
	rpcDir := filepath.Join(baseDir, "app", "rpc")

	if err := generateFile(protoDir, lowerName+".proto", grpcProtoTemplate, data); err != nil {
		return err
	}
	if err := generateFile(rpcDir, lowerName+"_server.go", grpcServerTemplate, data); err != nil {
		return err
	}

	return nil
}

// GenerateService 生成使用 Error Catalog 的 service
func GenerateService(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	return generateFile(dir, strings.ToLower(name)+"_service.go", serviceTemplate, templateData(name))
}

// validateName 驗證名稱安全性
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("scaffold: name cannot be empty")
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("scaffold: invalid name %q (only letters, digits, underscores allowed)", name)
	}
	if len(name) > 64 {
		return fmt.Errorf("scaffold: name too long (max 64 chars)")
	}
	return nil
}

// templateData 建立模板資料
func templateData(name string) map[string]string {
	return map[string]string{
		"Name":      capitalize(name),
		"LowerName": strings.ToLower(name),
	}
}

// generateFile 建立目錄並生成檔案
func generateFile(dir, filename, tmplStr string, data interface{}) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("scaffold: failed to create directory: %w", err)
	}

	tmpl, err := template.New("scaffold").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("scaffold: failed to parse template: %w", err)
	}

	path := filepath.Join(dir, filename)

	// 不覆蓋已存在的檔案
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("scaffold: file already exists: %s", path)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("scaffold: failed to create file: %w", err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("scaffold: failed to execute template: %w", err)
	}

	return nil
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
