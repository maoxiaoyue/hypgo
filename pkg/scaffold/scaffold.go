// Package scaffold 提供智慧程式碼生成功能
// 生成的程式碼自動整合 Schema-first 路由、Error Catalog 和 Contract Testing
package scaffold

import (
	"bytes"
	"fmt"
	"go/format"
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

// GeneratedBy 為生成程式碼 @ai:generated 的 by= 值。程式碼出自模板而非 AI，
// 先前依 .hyp/llm.yaml 或掃描 *.md 的關鍵字猜測供應商（README 提到 OpenAI 就標 by=openai），出處不實
const GeneratedBy = "hypgo-scaffold"

// HypgoVersion 為生成的 go.mod require 的 hypgo 版本；hyp CLI 啟動時設為自身版本。
// 先前 CLI / desktop / gRPC 的 go.mod 模板寫死版本號，升版時容易遺漏
var HypgoVersion = "v0.9.2"

// GenerateController 生成 controller（只含 handler 邏輯，路由在 routers/ 中）
// 目標專案的 .hyp/comment.yaml 開啟 ai_comment 時，每個 struct 與 func 附完整 @ai 區塊
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

// GenerateRouterSetup 生成 routers/router.go 總入口（只在首次執行）。
// 名稱會寫進生成的程式碼，同樣須驗證（先前未驗證，含換行的名稱等於插入任意程式碼）
func GenerateRouterSetup(dir, name, moduleName string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if moduleName == "" {
		moduleName = "myapp"
	}
	data := map[string]string{
		"Name":       camelName(name),
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
	data["TableName"] = pluralize(strings.ToLower(name))
	// 先前取名稱第一個字母：Category 與 Comment 都是 "c"，兩個 model 一起 join 時查詢出錯
	data["Alias"] = strings.ToLower(name)
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
		filepath.Join(baseDir, "config"),
		filepath.Join(baseDir, "tools", "genctx"),
		filepath.Join(baseDir, ".hyp"),
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
		{filepath.Join(baseDir, "app", "commands"), "schema.go", cliSchemaTemplate},
		{filepath.Join(baseDir, "tools", "genctx"), "main.go", cliGenctxTemplate},
		{filepath.Join(baseDir, "config"), "config.yaml", cliConfigTemplate},
		{filepath.Join(baseDir, ".hyp"), "llm.yaml", LLMYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "comment.yaml", CommentYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "config.yaml", HypConfigYamlTemplate},
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
		filepath.Join(baseDir, "config"),
		filepath.Join(baseDir, "tools", "genctx"),
		filepath.Join(baseDir, ".hyp"),
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
		{filepath.Join(baseDir, "app", "views"), "schema.go", desktopSchemaTemplate},
		{filepath.Join(baseDir, "tools", "genctx"), "main.go", desktopGenctxTemplate},
		{filepath.Join(baseDir, "config"), "config.yaml", desktopConfigTemplate},
		{filepath.Join(baseDir, ".hyp"), "llm.yaml", LLMYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "comment.yaml", CommentYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "config.yaml", HypConfigYamlTemplate},
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
		filepath.Join(baseDir, "config"),
		filepath.Join(baseDir, ".hyp"),
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
		{filepath.Join(baseDir, "app", "rpc"), "schema.go", grpcSchemaTemplate},
		{filepath.Join(baseDir, "config"), "config.yaml", grpcConfigTemplate},
		{filepath.Join(baseDir, ".hyp"), "llm.yaml", LLMYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "comment.yaml", CommentYamlTemplate},
		{filepath.Join(baseDir, ".hyp"), "config.yaml", HypConfigYamlTemplate},
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
// 目標專案的 .hyp/comment.yaml 開啟 ai_comment 時，每個 struct 與 func 附完整 @ai 區塊
func GenerateService(dir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	data := templateData(name)
	return generateFile(dir, strings.ToLower(name)+"_service.go", serviceTemplate, data)
}

// ValidateName 驗證資源／專案名稱（字母開頭，只含字母、數字、底線，最長 64 字元）；
// 供 CLI 在取用名稱前先行檢查（hyp generate 先前對空名稱直接 panic）
func ValidateName(name string) error {
	return validateName(name)
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
//   - Name         Go 識別字用的 CamelCase 名稱（user_profile → UserProfile）
//   - LowerName    全小寫的原始名稱，用於路徑、檔名、指令名
//   - Date         建立當下的日期（YYYY-MM-DD），供 @ai:generated 使用
//   - AIProvider   @ai:generated 的 by= 值（固定為 GeneratedBy）
//   - HypgoVersion go.mod require 的 hypgo 版本
//
// Name 先前只把首字母大寫：user_profile 產生 User_profileController 這類不合慣例的識別字；
// gRPC 更會編譯失敗——protoc-gen-go 把 message CreateUser_profileRequest 轉成 Go 型別
// CreateUserProfileRequest，模板卻引用 pb.CreateUser_profileRequest
func templateData(name string) map[string]string {
	return map[string]string{
		"Name":         camelName(name),
		"LowerName":    strings.ToLower(name),
		"Date":         time.Now().Format("2006-01-02"),
		"AIProvider":   GeneratedBy,
		"HypgoVersion": HypgoVersion,
	}
}

// generateFile 以模板生成新檔案（不覆蓋既有檔案）。
// 先在記憶體渲染；.go 檔依目標專案的 comment.yaml 決定是否保留 @ai 註解，再經 gofmt
// （模板的欄位對齊依名稱長度而變，先前部分生成檔過不了 gofmt -l；產出不合法的 Go 時直接回錯）；
// 最後以 O_EXCL 一次寫入並檢查 Close。先前 Stat 後才 Create、忽略 Close 錯誤，
// 模板執行或寫入失敗時留下寫到一半的檔案，重跑只會得到 "file already exists"
func generateFile(dir, filename, tmplStr string, data interface{}) error {
	tmpl, err := template.New("scaffold").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("scaffold: failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("scaffold: failed to execute template: %w", err)
	}
	content := buf.Bytes()

	path := filepath.Join(dir, filename)
	if strings.HasSuffix(filename, ".go") {
		if !aiCommentEnabled(dir) {
			content = stripAIComments(content)
		}
		if content, err = format.Source(content); err != nil {
			return fmt.Errorf("scaffold: generated invalid Go for %s: %w", path, err)
		}
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("scaffold: failed to create directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		return fmt.Errorf("scaffold: file already exists: %s", path)
	}
	if err != nil {
		return fmt.Errorf("scaffold: failed to create file: %w", err)
	}
	_, werr := file.Write(content)
	if cerr := file.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return fmt.Errorf("scaffold: failed to write %s: %w", path, werr)
	}
	return nil
}

// aiCommentEnabled 自 dir 往上找第一個含 .hyp/ 的目錄，讀其 comment.yaml 的 ai_comment
// （檔案不存在時預設 false，與 pkg/config 一致）。先前不論開關，生成的 controller / service
// 一律帶完整 @ai 區塊，與專案自己的 comment.yaml 互相矛盾
func aiCommentEnabled(dir string) bool {
	d, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if info, err := os.Stat(filepath.Join(d, ".hyp")); err == nil && info.IsDir() {
			data, err := os.ReadFile(filepath.Join(d, ".hyp", "comment.yaml"))
			if err != nil {
				return false
			}
			var cfg struct {
				AIComment bool `yaml:"ai_comment"`
			}
			return yaml.Unmarshal(data, &cfg) == nil && cfg.AIComment
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

// stripAIComments 移除 "// @ai:" 註解行，以及因此落單在註解區塊結尾的 "//" 空行
func stripAIComments(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "// @ai:") {
			kept = append(kept, l)
		}
	}
	out := make([]string, 0, len(kept))
	for i, l := range kept {
		if strings.TrimSpace(l) == "//" && (i+1 == len(kept) || !strings.HasPrefix(strings.TrimSpace(kept[i+1]), "//")) {
			continue
		}
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n"))
}

// camelName 把名稱轉成 CamelCase 的 Go 識別字：以底線分段、各段首字大寫（user_profile → UserProfile、API → API）
func camelName(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "_") {
		b.WriteString(capitalize(part))
	}
	return b.String()
}

// pluralize 英文名詞複數（資料表名用）。先前一律加 s：categorys、statuss、boxs
func pluralize(s string) string {
	switch {
	case len(s) > 1 && strings.HasSuffix(s, "y") && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	}
	return s + "s"
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
