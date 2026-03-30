package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate [type] [name]",
	Short: "Generate code for controllers, models, or services",
	Long: `Generate boilerplate code that follows HypGo conventions.

Available types:
  controller    Controller (handler) + Router (Schema routes) + Middleware setup
  model         Bun ORM model + Request/Response structs
  service       Service layer with Error Catalog

Generated file locations:
  controller → app/controllers/<name>_controller.go   (handlers only)
             + app/routers/<name>.go                   (Schema routes)
             + app/routers/router.go                   (setup entry, first time only)
             + app/routers/middleware.go                (middleware config, first time only)
  model      → app/models/<name>.go
  service    → app/services/<name>_service.go

Examples:
  hyp generate controller user
  hyp generate model order
  hyp generate service payment`,
	Args: cobra.ExactArgs(2),
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().StringP("module", "m", "", "Go module name (auto-detected from go.mod)")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	genType := args[0]
	name := args[1]

	moduleName, _ := cmd.Flags().GetString("module")
	if moduleName == "" {
		moduleName = detectModuleName()
	}

	switch genType {
	case "controller":
		return generateControllerFull(name, moduleName)
	case "model":
		return generateModel(name)
	case "service":
		return generateService(name, moduleName)
	default:
		return fmt.Errorf("unknown type: %s (use controller, model, or service)", genType)
	}
}

// generateControllerFull 生成 controller + router + middleware（一次到位）
func generateControllerFull(name, moduleName string) error {
	lowerName := strings.ToLower(name)
	capName := strings.ToUpper(name[:1]) + name[1:]

	// 1. Controller（handler 邏輯）
	if err := scaffold.GenerateController("app/controllers", name, moduleName); err != nil {
		return err
	}
	fmt.Printf("  + app/controllers/%s_controller.go\n", lowerName)

	// 2. Router（Schema 路由定義）
	if err := scaffold.GenerateRouter("app/routers", name, moduleName); err != nil {
		return err
	}
	fmt.Printf("  + app/routers/%s.go\n", lowerName)

	// 3. Router setup（首次才生成）
	if err := scaffold.GenerateRouterSetup("app/routers", name, moduleName); err == nil {
		fmt.Printf("  + app/routers/router.go\n")
	}
	// 已存在則跳過，不報錯

	// 4. Middleware（首次才生成）
	if err := scaffold.GenerateMiddleware("app/routers"); err == nil {
		fmt.Printf("  + app/routers/middleware.go\n")
	}

	fmt.Printf("\n✅ Controller generated: %s\n", capName)
	fmt.Printf("   Next steps:\n")
	fmt.Printf("   1. Run: hyp generate model %s\n", lowerName)
	fmt.Printf("   2. Edit app/routers/router.go → add: Register%sRoutes(r)\n", capName)
	fmt.Printf("   3. In main.go → call: routers.Setup(srv.Router())\n")

	return nil
}

func generateModel(name string) error {
	lowerName := strings.ToLower(name)
	capName := strings.ToUpper(name[:1]) + name[1:]

	if err := scaffold.GenerateModel("app/models", name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/models/%s.go\n", lowerName)
	fmt.Printf("   Includes: %s, Create%sReq, Update%sReq, %sResp, %sListResp\n",
		capName, capName, capName, capName, capName)
	return nil
}

func generateService(name, moduleName string) error {
	lowerName := strings.ToLower(name)

	if err := scaffold.GenerateService("app/services", name); err != nil {
		return err
	}
	fmt.Printf("✅ Generated: app/services/%s_service.go\n", lowerName)
	return nil
}

// detectModuleName 從 go.mod 中自動偵測 module 名稱
func detectModuleName() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "myapp"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return "myapp"
}
