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
	"time"

	"gopkg.in/yaml.v3"
)

// validName 只允許字母、數字、底線（防止目錄穿越和 code injection）
var validName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// DefaultAIProvider 是無法取得供應商資訊時的預設值。
const DefaultAIProvider = "unknown"

// llmYAMLConfig 是讀取 llm.yaml 所需的最小結構，
// 避免直接引入 pkg/config 造成不必要的套件依賴。
type llmYAMLConfig struct {
	Mode   string `yaml:"mode"`
	Ollama struct {
		Model string `yaml:"model"`
	} `yaml:"ollama"`
	API struct {
		Provider string `yaml:"provider"`
	} `yaml:"api"`
	RAG struct {
		GeneratorModel string `yaml:"generator_model"`
	} `yaml:"rag"`
}

// resolveAIProvider 在執行期決定 @ai 註解所記錄的供應商名稱。
// 解析優先順序：
//  1. config/llm.yaml 或 .hyp/llm.yaml（讀取配置的 LLM 供應商）
//  2. 掃描專案根目錄的 *.md 檔案中的 AI 供應商關鍵字
//  3. 退回 DefaultAIProvider（"unknown"）
func resolveAIProvider() string {
	if p := providerFromLLMConfig(); p != "" {
		return p
	}
	if p := providerFromMarkdown(); p != "" {
		return p
	}
	return DefaultAIProvider
}

// providerFromLLMConfig 從 config/llm.yaml 或 .hyp/llm.yaml 讀取供應商。
func providerFromLLMConfig() string {
	for _, path := range []string{"config/llm.yaml", ".hyp/llm.yaml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg llmYAMLConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil || cfg.Mode == "" || cfg.Mode == "none" {
			continue
		}
		switch cfg.Mode {
		case "api":
			return normalizeAPIProvider(cfg.API.Provider)
		case "ollama":
			if cfg.Ollama.Model != "" {
				return "ollama(" + cfg.Ollama.Model + ")"
			}
			return "ollama"
		case "rag":
			if cfg.RAG.GeneratorModel != "" {
				return "ollama(" + cfg.RAG.GeneratorModel + ")"
			}
			return "ollama"
		}
	}
	return ""
}

// normalizeAPIProvider 將 llm.yaml 的 provider 值對應到標準名稱。
func normalizeAPIProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "claude"
	case "openai":
		return "openai"
	case "gemini":
		return "google"
	default:
		if provider != "" {
			return provider
		}
		return ""
	}
}

// providerFromMarkdown 掃描專案根目錄的 *.md 檔案，比對 AI 供應商關鍵字。
// 優先順序依序：claude → openai → google/gemini → ollama/llama
func providerFromMarkdown() string {
	type hint struct {
		keyword  string
		provider string
	}
	hints := []hint{
		{"claude", "claude"},
		{"anthropic", "claude"},
		{"openai", "openai"},
		{"chatgpt", "openai"},
		{"gpt-4", "openai"},
		{"gpt-3", "openai"},
		{"gemini", "google"},
		{"ollama", "ollama"},
		{"llama3", "ollama"},
		{"llama2", "ollama"},
		{"mistral", "ollama"},
	}

	files, _ := filepath.Glob("*.md")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		for _, h := range hints {
			if strings.Contains(lower, h.keyword) {
				return h.provider
			}
		}
	}
	return ""
}

// GenerateController 生成 controller（只含 handler 邏輯，路由在 routers/ 中）
// 產生的所有 struct 與 func 均強制加入 // @ai: madeby <provider> 註解，不可關閉。
func GenerateController(dir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = "myapp"
	}
	data := templateData(name)
	data["ModuleName"] = moduleName
	data["AIProvider"] = resolveAIProvider()
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
		{filepath.Join(baseDir, "app", "config"), "llm.yaml", LLMYamlTemplate},
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
		{filepath.Join(baseDir, "app", "config"), "llm.yaml", LLMYamlTemplate},
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
		{filepath.Join(baseDir, "app", "config"), "llm.yaml", LLMYamlTemplate},
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
// 產生的所有 struct 與 func 均強制加入 // @ai: madeby <provider> 註解，不可關閉。
func GenerateService(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	data := templateData(name)
	data["AIProvider"] = resolveAIProvider()
	return generateFile(dir, strings.ToLower(name)+"_service.go", serviceTemplate, data)
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

// templateData 建立模板資料。
// 所有模板共用欄位：
//   - Name       首字母大寫的資源名稱
//   - LowerName  全小寫的資源名稱
//   - Date       建立當下的日期（YYYY-MM-DD），供 @ai:generated 使用
//   - AIProvider 目前偵測到的 AI 供應商名稱，供 @ai:generated 使用
//
// @ai:generated by=hypgo date=2026-04-23
// @ai:purpose 統一所有 scaffold 模板可用的資料欄位，避免模板引用未設定變數
// @ai:input name 資源名稱字串
// @ai:output 已填妥共用欄位的 map
// @ai:sideeffect 讀取 config/llm.yaml 或 *.md 以偵測 AI 供應商
func templateData(name string) map[string]string {
	return map[string]string{
		"Name":       capitalize(name),
		"LowerName":  strings.ToLower(name),
		"Date":       time.Now().Format("2006-01-02"),
		"AIProvider": resolveAIProvider(),
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
