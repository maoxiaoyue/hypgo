// @chris
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var generateCmd = &cobra.Command{
	Use:   "generate [type] [name]",
	Short: "Generate code for controllers, models, services, or commands",
	Long: `Generate boilerplate code that follows HypGo conventions.

Available types:
  controller    Controller (handler) + Router (Schema routes) + Middleware;
                also the model and service it depends on when they don't exist yet
  model         Bun ORM model + Request/Response structs
  service       Service layer with Error Catalog
  command       CLI subcommand (Cobra) for CLI projects
  view          Desktop GUI view (Fyne) for desktop projects
  proto         Protobuf service definition + gRPC server for gRPC projects

Generated file locations (relative to the project root, the nearest go.mod going up):
  controller → app/controllers/<name>_controller.go + routers/<name>.go
  model      → app/models/<name>.go
  service    → app/services/<name>_service.go
  command    → app/commands/<name>.go
  view       → app/views/<name>_view.go
  proto      → app/proto/<name>pb/<name>.proto + app/rpc/<name>_server.go

Names are letters, digits and underscores; identifiers are CamelCase
(order_item → OrderItem) and routes are plural (/api/order_items). Existing
files are never overwritten, and nothing is written when the new code would
clash with existing identifiers or error codes.

Generated endpoints answer 501 until the service is implemented and injected
into the controller (see the comment in routers/<name>.go).

Examples:
  hyp generate controller user
  hyp generate model order_item
  hyp generate service payment
  hyp generate command process`,
	Args: cobra.ExactArgs(2),
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().StringP("module", "m", "", "Go module name (auto-detected from go.mod)")
}

// generator 一次 hyp generate 的目標：專案根目錄、資源名稱與 module 路徑
type generator struct {
	root, name, module string
}

func runGenerate(cmd *cobra.Command, args []string) error {
	genType := args[0]
	name := args[1]

	// 先驗證名稱：各 generate* 會先取 name[:1]，先前空字串直接 panic（驗證在 scaffold 內、太晚）
	if err := scaffold.ValidateName(name); err != nil {
		return err
	}

	// 先前直接寫到工作目錄：在 app/ 下執行會生成到 app/app/services/，
	// 不在專案內則用 myapp 當 module 產生一份編不過的程式碼
	root, err := requireModuleRoot()
	if err != nil {
		return fmt.Errorf("%w (hyp generate writes into the project's app/ and routers/)", err)
	}

	moduleName, _ := cmd.Flags().GetString("module")
	if moduleName == "" {
		if moduleName, err = detectModuleName(root); err != nil {
			return err
		}
	} else if err := module.CheckImportPath(moduleName); err != nil {
		// --module 會原樣寫進生成程式碼的 import 字串
		return fmt.Errorf("invalid --module %q: %w", moduleName, err)
	}

	// hyp new --force 可能在同一行程留下「保留既有檔案」；generate 遇到既有檔案一律回錯
	scaffold.KeepExisting = false

	g := generator{root: root, name: name, module: moduleName}
	switch genType {
	case "controller":
		return g.controller()
	case "model":
		return g.model()
	case "service":
		return g.service()
	case "command":
		return g.command()
	case "view":
		return g.view()
	case "proto":
		return g.proto()
	default:
		return fmt.Errorf("unknown type: %s (use controller, model, service, command, view, or proto)", genType)
	}
}

// dir 回傳專案根目錄下的路徑
func (g generator) dir(elem ...string) string {
	return filepath.Join(append([]string{g.root}, elem...)...)
}

// lower 是檔名用的小寫名稱；camel 是生成程式碼實際使用的識別字。
// 先前訊息只把首字母大寫：order_item 提示 RegisterOrder_itemRoutes，實際函式是 RegisterOrderItemRoutes
func (g generator) lower() string { return strings.ToLower(g.name) }
func (g generator) camel() string { return scaffold.CamelName(g.name) }

// controller 生成 controller + router + middleware（Web / API 專案），以及它依賴而尚未存在的 model 與 service
func (g generator) controller() error {
	// 先確認要寫的檔案都不存在再動手。先前 hyp generate controller router 寫完 controller 才發現
	// routers/router.go 已存在而失敗，重跑又卡在 controller 已存在
	for _, rel := range []string{
		filepath.Join("app", "controllers", g.lower()+"_controller.go"),
		filepath.Join("routers", g.lower()+".go"),
	} {
		if fileExists(g.dir(rel)) {
			return fmt.Errorf("%s already exists; nothing was generated", filepath.ToSlash(rel))
		}
	}

	// controller import models 與 services：缺的一併生成，否則單獨生成的 controller 編不過
	if !fileExists(g.dir("app", "models", g.lower()+".go")) {
		if err := scaffold.GenerateModel(g.dir("app", "models"), g.name); err != nil {
			return err
		}
		fmt.Printf("  + app/models/%s.go\n", g.lower())
	}
	if !fileExists(g.dir("app", "services", g.lower()+"_service.go")) {
		if err := scaffold.GenerateService(g.dir("app", "services"), g.name, g.module); err != nil {
			return err
		}
		fmt.Printf("  + app/services/%s_service.go\n", g.lower())
	}

	if err := scaffold.GenerateController(g.dir("app", "controllers"), g.name, g.module); err != nil {
		return err
	}
	fmt.Printf("  + app/controllers/%s_controller.go\n", g.lower())

	if err := scaffold.GenerateRouter(g.dir("routers"), g.name, g.module); err != nil {
		return err
	}
	fmt.Printf("  + routers/%s.go\n", g.lower())

	// router.go / middleware.go 只在第一次生成；已存在就不動。
	// 先前不分原因吞掉所有錯誤（權限不足等也當成「已存在」）
	setupCreated := false
	if !fileExists(g.dir("routers", "router.go")) {
		if err := scaffold.GenerateRouterSetup(g.dir("routers"), g.name, g.module); err != nil {
			return err
		}
		setupCreated = true
		fmt.Printf("  + routers/router.go\n")
	}
	if !fileExists(g.dir("routers", "middleware.go")) {
		if err := scaffold.GenerateMiddleware(g.dir("routers")); err != nil {
			return err
		}
		fmt.Printf("  + routers/middleware.go\n")
	}

	fmt.Printf("\n✅ Controller generated: %sController\n", g.camel())
	fmt.Printf("   Next steps:\n")
	fmt.Printf("   1. Edit routers/router.go → add to Setup: Register%sRoutes(r)\n", g.camel())
	fmt.Printf("   2. Implement app/services/%s_service.go and inject it in routers/%s.go (endpoints answer 501 until then)\n", g.lower(), g.lower())
	if setupCreated {
		fmt.Printf("   3. In main.go → call: routers.Setup(app.Server().Router())\n")
	}
	return nil
}

func (g generator) model() error {
	if err := scaffold.GenerateModel(g.dir("app", "models"), g.name); err != nil {
		return err
	}
	c := g.camel()
	fmt.Printf("✅ Generated: app/models/%s.go\n", g.lower())
	fmt.Printf("   Includes: %s, Create%sReq, Update%sReq, %sResp, %sListResp\n", c, c, c, c, c)
	return nil
}

func (g generator) service() error {
	if err := scaffold.GenerateService(g.dir("app", "services"), g.name, g.module); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/services/%s_service.go\n", g.lower())
	if !fileExists(g.dir("app", "models", g.lower()+".go")) {
		fmt.Printf("   It uses models.%sResp and friends: run hyp generate model %s\n", g.camel(), g.lower())
	}
	return nil
}

// command 生成 CLI 子命令（CLI 專案用）
func (g generator) command() error {
	// 與專案同名的子命令和根命令在 Schema registry 用同一個名稱：兩者內容不同，啟動時 panic
	if root := filepath.Base(g.module); strings.EqualFold(g.name, root) {
		return fmt.Errorf("%q is the name of the root command; choose another name for the subcommand", root)
	}
	if err := scaffold.GenerateCommand(g.dir("app", "commands"), g.name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/commands/%s.go\n", g.lower())
	fmt.Printf("   Command: %s %s (registered with the root command and the Schema registry in its init)\n", filepath.Base(g.module), g.lower())
	fmt.Printf("   Edit app/commands/%s.go to implement run%s\n", g.lower(), g.camel())
	return nil
}

// view 生成 Desktop GUI view（Desktop 專案用）
func (g generator) view() error {
	if err := scaffold.GenerateView(g.dir("app", "views"), g.name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/views/%s_view.go\n", g.lower())
	fmt.Printf("   Function: views.%sView(w)\n", g.camel())
	return nil
}

// proto 生成 .proto 定義 + gRPC server（gRPC 專案用）
func (g generator) proto() error {
	if err := scaffold.GenerateProto(g.root, g.name, g.module); err != nil {
		return err
	}
	fmt.Printf("  + app/proto/%spb/%s.proto\n", g.lower(), g.lower())
	fmt.Printf("  + app/rpc/%s_server.go\n", g.lower())
	fmt.Printf("\n✅ Proto generated: %s\n", g.lower())
	fmt.Printf("   Next steps:\n")
	fmt.Printf("   1. make proto    (compiles every .proto under app/proto)\n")
	fmt.Printf("   2. In main.go → register the service on srv.GRPCServer() with rpc.New%sServer()\n", g.camel())
	fmt.Printf("   3. In app/rpc/schema.go → add its methods to RegisterSchemas()\n")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// detectModuleName 讀取專案根目錄 go.mod 的 module 路徑
func detectModuleName(root string) (string, error) {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mod := modfile.ModulePath(data)
	if mod == "" {
		return "", fmt.Errorf("%s has no module directive; pass --module", path)
	}
	return mod, nil
}
