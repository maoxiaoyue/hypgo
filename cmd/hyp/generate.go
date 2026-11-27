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
	// 先前少給參數只回 "accepts 2 arg(s), received 1"，看不出該給什麼
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("usage: hyp generate <type> <name> (types: %s)", generateTypes)
		}
		return nil
	},
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().StringP("module", "m", "", "Go module name (auto-detected from go.mod)")
}

const generateTypes = "controller, model, service, command, view, proto"

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

	// 生成的程式碼 import 專案自己的套件，路徑只能是 go.mod 的 module。
	// 先前 --module 給了別的路徑就照寫，產生的 import 一定對不上
	moduleName, err := detectModuleName(root)
	if flagModule, _ := cmd.Flags().GetString("module"); flagModule != "" {
		if cerr := module.CheckImportPath(flagModule); cerr != nil {
			return fmt.Errorf("invalid --module %q: %w", flagModule, cerr)
		}
		if err == nil && flagModule != moduleName {
			return fmt.Errorf("--module %q does not match the module in go.mod (%q); the generated imports would not resolve. Drop the flag", flagModule, moduleName)
		}
		moduleName, err = flagModule, nil // go.mod 沒有 module 指令時才用得到
	}
	if err != nil {
		return err
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
		return fmt.Errorf("unknown type: %s (types: %s)", genType, generateTypes)
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
	// 這兩個名稱的資源檔會佔掉 routers/router.go、routers/middleware.go：Setup / APIMiddleware 不會生成
	if g.lower() == "router" || g.lower() == "middleware" {
		return fmt.Errorf("%q cannot be a controller name: routers/%s.go is reserved for the generated %s; nothing was generated", g.name, g.lower(),
			map[string]string{"router": "Setup function", "middleware": "middleware chains"}[g.lower()])
	}

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

	// 沿用既有的 model / service 之前，確認它有 controller 要用的名稱。
	// 先前手寫的 app/models/user.go 只有 User 時照樣生成，service 與 controller 引用不存在的 models.UserResp
	c := g.camel()
	for _, need := range []struct {
		file, hint string
		names      []string
	}{
		{filepath.Join("app", "models", g.lower()+".go"), "model", []string{c, "Create" + c + "Req", "Update" + c + "Req", c + "Resp", c + "ListResp"}},
		{filepath.Join("app", "services", g.lower()+"_service.go"), "service", []string{c + "Service", "New" + c + "Service",
			"Err" + c + "NotFound", "Err" + c + "Invalid", "Err" + c + "NotImplemented", "Err" + c + "Internal"}},
	} {
		if !fileExists(g.dir(need.file)) {
			continue
		}
		if missing := scaffold.MissingDecls(filepath.Dir(g.dir(need.file)), need.names...); len(missing) > 0 {
			return fmt.Errorf("%s exists but its package does not declare %s, which the generated controller uses; nothing was generated. Add them, or move the file away and let hyp generate the %s",
				filepath.ToSlash(need.file), strings.Join(missing, ", "), need.hint)
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
	fmt.Printf("   2. Implement app/services/%s_service.go (endpoints answer 501 until then)\n", g.lower())
	fmt.Printf("   3. Inject the service; db and log have to be passed down from main.go, three places to edit:\n")
	fmt.Printf("        main.go              routers.Setup(app.Server().Router(), db, log)\n")
	fmt.Printf("        routers/router.go    Setup(r, db, log) → Register%sRoutes(r, db, log)\n", g.camel())
	fmt.Printf("        routers/%s.go    take db and log, build the controller with services.New%sService(db, log)\n", g.lower(), g.camel())
	if setupCreated {
		fmt.Printf("   4. In main.go → call: routers.Setup(app.Server().Router())\n")
	}
	tidyHint()
	return nil
}

func (g generator) model() error {
	if err := scaffold.GenerateModel(g.dir("app", "models"), g.name); err != nil {
		return err
	}
	c := g.camel()
	fmt.Printf("✅ Generated: app/models/%s.go\n", g.lower())
	fmt.Printf("   Includes: %s, Create%sReq, Update%sReq, %sResp, %sListResp\n", c, c, c, c, c)
	tidyHint()
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
	tidyHint()
	return nil
}

// command 生成 CLI 子命令（CLI 專案用）
func (g generator) command() error {
	// 與專案同名的子命令和根命令在 Schema registry 用同一個名稱：兩者內容不同，啟動時 panic
	// 根命令的名稱與 hyp new 取的相同（module 路徑結尾的 /v2 不算）。先前取最後一段：
	// example.com/tools/v2 誤擋 v2，而放行的 tools 讓程式啟動即 panic
	root := moduleBaseName(g.module)
	if strings.EqualFold(g.name, root) {
		return fmt.Errorf("%q is the name of the root command; choose another name for the subcommand", root)
	}
	// 子命令掛在 rootCmd 上：不是 CLI 專案時生成的檔案編不過
	if missing := scaffold.MissingDecls(g.dir("app", "commands"), "rootCmd"); len(missing) > 0 {
		return fmt.Errorf("app/commands does not declare rootCmd: hyp generate command is for CLI projects (hyp new cli); nothing was generated")
	}
	if err := scaffold.GenerateCommand(g.dir("app", "commands"), g.name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/commands/%s.go\n", g.lower())
	fmt.Printf("   Command: %s %s (registered with the root command and the Schema registry in its init)\n", root, g.lower())
	fmt.Printf("   Edit app/commands/%s.go to implement run%s\n", g.lower(), g.camel())
	return nil
}

// view 生成 Desktop GUI view（Desktop 專案用）
func (g generator) view() error {
	if err := g.requireModule("fyne.io/fyne/v2", "view", "desktop"); err != nil {
		return err
	}
	if err := scaffold.GenerateView(g.dir("app", "views"), g.name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/views/%s_view.go\n", g.lower())
	fmt.Printf("   Function: views.%sView(w)\n", g.camel())
	fmt.Printf("   Register it in app/views/schema.go: schema.RegisterDesktop(\"view/%s\", \"%s view\", nil, nil)\n", g.lower(), g.camel())
	return nil
}

// proto 生成 .proto 定義 + gRPC server（gRPC 專案用）
func (g generator) proto() error {
	if err := g.requireModule("google.golang.org/grpc", "proto", "grpc"); err != nil {
		return err
	}
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

// requireModule 確認專案的 go.mod require 了 path；不是對應類型的專案時生成的檔案編不過。
// 先前在 API 專案執行 hyp generate view / proto 照樣成功，之後 go build ./... 失敗
func (g generator) requireModule(path, genType, project string) error {
	data, err := os.ReadFile(g.dir("go.mod"))
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	for _, r := range f.Require {
		if r.Mod.Path == path {
			return nil
		}
	}
	return fmt.Errorf("go.mod does not require %s: hyp generate %s is for %s projects (hyp new %s); nothing was generated", path, genType, project, project)
}

// tidyHint：生成的程式碼可能 import 專案還沒用到的套件（CLI 專案裡的 hidb、bun）
func tidyHint() {
	fmt.Printf("   If the build reports a missing module or go.sum entry: go mod tidy\n")
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
