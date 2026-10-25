// @chris
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
	"golang.org/x/mod/module"
)

var newCmd = &cobra.Command{
	Use:   "new [type] [project-name]",
	Short: "Create a new HypGo project",
	Long: `Create a new HypGo project. Specify the project type:

  hyp new <name>              Full-stack web project (default)
  hyp new web <name>          Full-stack web project (explicit)
  hyp new cli <name>          CLI tool project (Cobra + services)
  hyp new desktop <name>      Desktop application (Fyne GUI + services)
  hyp new grpc <name>         gRPC microservice (Protobuf + services)

Examples:
  hyp new myapp               Full-stack web project
  hyp new cli mytool           CLI tool project
  hyp new desktop mydesktop    Desktop application
  hyp new grpc userservice     gRPC microservice`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkNewProject(cmd, args[len(args)-1]); err != nil {
			return err
		}
		if len(args) == 1 {
			// hyp new <name> → web 專案（預設）
			return runNew(cmd, args)
		}
		// hyp new <type> <name>
		projectType := args[0]
		projectName := args[1]
		switch projectType {
		case "web":
			return runNew(cmd, []string{projectName})
		case "cli":
			return runNewCLI(projectName)
		case "desktop":
			return runNewDesktop(projectName, false)
		case "grpc":
			return runNewGRPC(projectName)
		default:
			return fmt.Errorf("unknown project type: %s (use web, cli, desktop, or grpc)", projectType)
		}
	},
}

var newCLICmd = &cobra.Command{
	Use:   "cli [project-name]",
	Short: "Create a new CLI tool project",
	Long: `Create a new CLI tool project with Cobra command structure.

Generated structure:
  mytool/
  ├── app/
  │   ├── commands/      CLI subcommands (Cobra)
  │   │   └── root.go    Root command
  │   ├── models/        Data structures
  │   └── services/      Business logic + Error Catalog
  ├── config/config.yaml Runtime config (loaded in main.go → commands.Cfg)
  ├── main.go            Entry point
  └── go.mod

After creation:
  cd mytool && go mod tidy && go run .

Add subcommands:
  hyp generate command process
  hyp generate command export`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkNewProject(cmd, args[0]); err != nil {
			return err
		}
		return runNewCLI(args[0])
	},
}

var newDesktopCmd = &cobra.Command{
	Use:   "desktop [project-name]",
	Short: "Create a new Desktop application (Fyne GUI)",
	Long: `Create a new Desktop application using Fyne GUI toolkit.

Generated structure:
  myapp/
  ├── app/
  │   ├── views/         GUI views (Fyne widgets + layouts)
  │   │   └── main_view.go
  │   ├── models/        Data structures
  │   └── services/      Business logic + Error Catalog
  ├── config/config.yaml Runtime config (loaded in main.go)
  ├── main.go            Entry point (config → logger → Fyne app + window)
  └── go.mod

After creation:
  cd myapp && go mod tidy && go run .

Add views:
  hyp generate view settings
  hyp generate view dashboard

Requires: C compiler (gcc) for CGO (Fyne dependency).
The toolchain is checked after creation; pass --install-deps to let hyp
run the platform's install command (winget / xcode-select / apt-get / dnf / pacman).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		installDeps, _ := cmd.Flags().GetBool("install-deps")
		if err := checkNewProject(cmd, args[0]); err != nil {
			return err
		}
		return runNewDesktop(args[0], installDeps)
	},
}

var newGRPCCmd = &cobra.Command{
	Use:   "grpc [project-name]",
	Short: "Create a new gRPC microservice project",
	Long: `Create a new gRPC microservice project with Protobuf definitions.

Generated structure:
  myservice/
  ├── app/
  │   ├── proto/<name>pb/    Protobuf definitions (.proto)
  │   ├── rpc/               gRPC server implementations
  │   ├── models/            Data structures
  │   └── services/          Business logic + Error Catalog
  ├── config/config.yaml     Runtime config: server.addr / server.tls (loaded in main.go)
  ├── main.go                gRPC server entry point
  ├── Makefile               protoc compilation commands
  └── go.mod

After creation:
  cd myservice && go mod tidy
  make proto                 Compile .proto → Go code
  go run .

Add more services:
  hyp generate proto order

Requires: protoc + protoc-gen-go + protoc-gen-go-grpc`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkNewProject(cmd, args[0]); err != nil {
			return err
		}
		return runNewGRPC(args[0])
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newCLICmd)
	newCmd.AddCommand(newDesktopCmd)
	newCmd.AddCommand(newGRPCCmd)

	newDesktopCmd.Flags().Bool("install-deps", false, "Install the C toolchain Fyne needs via the platform package manager")
	newCmd.PersistentFlags().Bool("force", false, "Generate into an existing non-empty directory (existing files are never overwritten)")
}

// checkNewProject 建立專案前的共用檢查（hyp new 各類型與 hyp api）：名稱合法、目標目錄可用。
// 先前完全不檢查：hyp new ../escaped 寫到 CWD 之外並產生 module ../escaped，"my app" 產生 module my app；
// 對既有專案重跑 hyp new / hyp api 會截斷 go.mod、config.yaml 等既有檔案後才在半途失敗
func checkNewProject(cmd *cobra.Command, name string) error {
	if err := validateProjectName(name); err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	fi, err := os.Stat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s already exists and is not a directory", name)
	}
	entries, err := os.ReadDir(name)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("directory %s already exists and is not empty; choose another name or pass --force (existing files are never overwritten)", name)
	}
	return nil
}

// validateProjectName 專案名稱同時是目錄名稱與 go.mod 的 module 路徑：
// 必須是單一路徑元素，且是合法的 Go import path（不含空白、不以 . 開頭等）
func validateProjectName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid project name %q: must be a single directory name without path separators", name)
	}
	if err := module.CheckImportPath(name); err != nil {
		return fmt.Errorf("invalid project name %q: not a valid Go module path: %w", name, err)
	}
	return nil
}

// writeNewFile 建立新檔案；檔案已存在時回錯，不截斷（hyp new / hyp api --force 不覆寫既有檔案）
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("file already exists: %s", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runNewCLI 生成 CLI 專案
func runNewCLI(projectName string) error {
	if err := scaffold.GenerateCLIProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create CLI project: %w", err)
	}

	postScaffoldUpgrade(projectName)

	fmt.Printf("\n✨ Successfully created CLI project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── commands/\n")
	fmt.Printf("   │   │   ├── root.go\n")
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol cli）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── tools/\n")
	fmt.Printf("   │   └── genctx/          # 產生 .hyp/context.yaml（go run ./tools/genctx）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add subcommands:\n")
	fmt.Printf("   hyp generate command process\n")
	fmt.Printf("   hyp generate command export\n")
	fmt.Printf("\n🧩 Generate schema manifest (.hyp/context.yaml):\n")
	fmt.Printf("   go run ./tools/genctx\n")

	return nil
}

// runNewDesktop 生成 Desktop 專案（Fyne）。
// installDeps 為 true 時，缺少 C 工具鏈會代為執行平台的安裝命令。
func runNewDesktop(projectName string, installDeps bool) error {
	if err := scaffold.GenerateDesktopProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create desktop project: %w", err)
	}

	postScaffoldUpgrade(projectName)

	fmt.Printf("\n✨ Successfully created Desktop project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── views/\n")
	fmt.Printf("   │   │   ├── main_view.go\n")
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol desktop）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── tools/\n")
	fmt.Printf("   │   └── genctx/          # 產生 .hyp/context.yaml（go run ./tools/genctx）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add views:\n")
	fmt.Printf("   hyp generate view settings\n")
	fmt.Printf("   hyp generate view dashboard\n")
	fmt.Printf("\n🧩 Generate schema manifest (.hyp/context.yaml):\n")
	fmt.Printf("   go run ./tools/genctx\n")

	ensureDesktopDeps(installDeps)

	return nil
}

// runNewGRPC 生成 gRPC 微服務專案
func runNewGRPC(projectName string) error {
	if err := scaffold.GenerateGRPCProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create gRPC project: %w", err)
	}

	postScaffoldUpgrade(projectName)

	lowerName := strings.ToLower(projectName)
	fmt.Printf("\n✨ Successfully created gRPC project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── proto/%spb/\n", lowerName)
	fmt.Printf("   │   │   └── %s.proto\n", lowerName)
	fmt.Printf("   │   ├── rpc/\n")
	fmt.Printf("   │   │   ├── %s_server.go\n", lowerName)
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol grpc）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   ├── Makefile\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   make proto              # compile .proto → Go code\n")
	fmt.Printf("   go run .                # 啟動時自動寫 .hyp/context.yaml（gRPC schema manifest）\n")
	fmt.Printf("\n📝 Add more services:\n")
	fmt.Printf("   hyp generate proto order\n")
	fmt.Printf("\n⚠️  Requires: protoc + protoc-gen-go + protoc-gen-go-grpc\n")
	fmt.Printf("   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest\n")
	fmt.Printf("   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest\n")

	return nil
}

func runNew(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// 創建項目目錄結構
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "routers"), // 路由層置於專案根目錄（與 app/ 平行）
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, ".hyp"),
		filepath.Join(projectName, "logs"),
		filepath.Join(projectName, "static", "css"),
		filepath.Join(projectName, "static", "js"),
		filepath.Join(projectName, "static", "images"),
		filepath.Join(projectName, "templates"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// logs/ 是空目錄，git 不會追蹤；放 .gitkeep 讓 clone 後目錄仍存在，
	// logger 寫 logs/app.log 時不需先手動建目錄
	if err := createGitKeep(filepath.Join(projectName, "logs")); err != nil {
		return err
	}

	// 創建配置文件
	if err := createNewConfigFile(projectName); err != nil {
		return err
	}

	// 創建設計時設定檔（.hyp/llm.yaml + comment.yaml + config.yaml）
	if err := createLLMConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}
	if err := createCommentConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}
	if err := createHypConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}

	// @ai:generated 標記所用日期（YYYY-MM-DD）
	today := time.Now().Format("2006-01-02")

	// 創建示例 Model（Schema Input/Output DTO）
	if err := createHomeModel(projectName, today); err != nil {
		return err
	}

	// 創建示例 Controller（薄控制器，引用 model）
	if err := createFullStackController(projectName, today); err != nil {
		return err
	}

	// 創建 Router 入口（routers/router.go，引入 router + schema，串接 controller + model）
	if err := createRouterSetup(projectName, today); err != nil {
		return err
	}

	// 創建主程序文件（呼叫 routers.Setup）
	if err := createMainFile(projectName); err != nil {
		return err
	}

	// 創建歡迎頁面模板
	if err := createWelcomeTemplate(projectName); err != nil {
		return err
	}

	// 創建靜態資源
	if err := createStaticAssets(projectName); err != nil {
		return err
	}

	// 創建 go.mod
	if err := createGoMod(projectName); err != nil {
		return err
	}

	postScaffoldUpgrade(projectName)

	fmt.Printf("✨ Successfully created full-stack project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/    # home.go（薄控制器）\n")
	fmt.Printf("   │   ├── models/         # home.go（Schema Input/Output DTO）\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── routers/            # router.go（Setup：router + schema 串接）\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml\n")
	fmt.Printf("   ├── logs/               # .gitkeep（logger 輸出 logs/app.log）\n")
	fmt.Printf("   ├── static/\n")
	fmt.Printf("   │   ├── css/\n")
	fmt.Printf("   │   ├── js/\n")
	fmt.Printf("   │   └── images/\n")
	fmt.Printf("   ├── templates/\n")
	fmt.Printf("   │   └── welcome.html\n")
	fmt.Printf("   ├── go.mod\n")
	fmt.Printf("   └── main.go\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   hyp run\n")

	return nil
}

func createNewConfigFile(projectName string) error {
	configContent := `# HypGo Configuration File

server:
  protocol: http2  # http1, http2, http3
  addr: :8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120
  keep_alive: 30
  max_handlers: 1000
  max_concurrent_streams: 100
  max_read_frame_size: 1048576
  enable_graceful_restart: true  # 啟用熱重啟
  # 可信代理網段：服務跑在 nginx / LB 之後時務必設定，否則 c.ClientIP()
  # 只回傳實際連線來源（安全預設），不會解析 X-Forwarded-For / X-Real-IP，
  # IPWhitelist / RateLimiter 等以 ClientIP 為 key 的功能會全部失準。
  # 支援 CIDR 或單一 IP；本機開發／服務直接暴露時留空即可。
  # trusted_proxies:
  #   - 127.0.0.1
  #   - 10.0.0.0/8
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

database:
  driver: mysql  # mysql, postgres, tidb, redis
  dsn: "user:password@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local"
  max_idle_conns: 10
  max_open_conns: 100
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0

logger:
  level: debug  # debug, info, notice, warning, emergency
  output: logs/app.log  # stdout or file path
  colors: true
  rotation:
    max_size: 100MB
    max_age: 7d
    max_backups: 10
    compress: true
`

	filename := filepath.Join(projectName, "config", "config.yaml")
	return writeNewFile(filename, []byte(configContent), 0644)
}

// createLLMConfigFile 寫入 llm.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.LLMYamlTemplate，預設 mode=none（不啟用 LLM）。
func createLLMConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "llm.yaml")
	return writeNewFile(filename, []byte(scaffold.LLMYamlTemplate), 0644)
}

// createCommentConfigFile 寫入 comment.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.CommentYamlTemplate，預設只開啟 normal_comment。
func createCommentConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "comment.yaml")
	return writeNewFile(filename, []byte(scaffold.CommentYamlTemplate), 0644)
}

// createHypConfigFile 寫入 .hyp/config.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.HypConfigYamlTemplate，預設 language=en / debug_level=debug / environment=local。
func createHypConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "config.yaml")
	return writeNewFile(filename, []byte(scaffold.HypConfigYamlTemplate), 0644)
}

func createMainFile(projectName string) error {
	mainContent := "package main\n\n" +
		"import (\n" +
		"\t\"fmt\"\n" +
		"\t\"os\"\n\n" +
		"\t\"github.com/maoxiaoyue/hypgo\"\n\n" +
		"\t\"" + projectName + "/routers\"\n" +
		")\n\n" +
		"// configPath 是 runtime 設定檔位置（專案根目錄 config/；.hyp/ 下的是設計時設定，兩者分離）\n" +
		"const configPath = \"config/config.yaml\"\n\n" +
		"func main() {\n" +
		"\t// 嚴格載入設定：檔案不存在或內容無效即結束，不靜默退回預設值起跑；\n" +
		"\t// logger 與 server 依 config 建立（logger.level / output / colors / rotation 都會生效）\n" +
		"\tapp, err := hypgo.NewWithConfig(configPath)\n" +
		"\tif err != nil {\n" +
		"\t\tfmt.Fprintf(os.Stderr, \"load %s: %v\\n\", configPath, err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n\n" +
		"\t// 所有路由與中間件定義於 routers/router.go\n" +
		"\trouters.Setup(app.Server().Router())\n\n" +
		"\t// Run 會阻塞到收到 SIGINT/SIGTERM 並完成優雅關閉（Start() 內建，不必自己處理訊號）\n" +
		"\tif err := app.Run(); err != nil {\n" +
		"\t\tapp.Logger().Errorf(\"server error: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"}\n"

	filename := filepath.Join(projectName, "main.go")
	return writeNewFile(filename, []byte(mainContent), 0644)
}

// createHomeModel 生成 app/models/home.go，定義 Schema Input/Output 用的 DTO。
func createHomeModel(projectName, today string) error {
	content := "// Package models 定義資料模型與 Schema 的 Request/Response DTO。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package models\n\n" +
		"// HealthResp 健康檢查回應（Schema Output）。\n" +
		"type HealthResp struct {\n" +
		"\tSuccess bool   `json:\"success\"`\n" +
		"\tMessage string `json:\"message\"`\n" +
		"}\n\n" +
		"// InfoResp 伺服器資訊回應（Schema Output）。\n" +
		"type InfoResp struct {\n" +
		"\tSuccess bool       `json:\"success\"`\n" +
		"\tMessage string     `json:\"message\"`\n" +
		"\tData    InfoDetail `json:\"data\"`\n" +
		"}\n\n" +
		"// InfoDetail 伺服器資訊明細。\n" +
		"type InfoDetail struct {\n" +
		"\tVersion  string `json:\"version\"`\n" +
		"\tProtocol string `json:\"protocol\"`\n" +
		"}\n"

	filename := filepath.Join(projectName, "app", "models", "home.go")
	return writeNewFile(filename, []byte(content), 0644)
}

// createRouterSetup 生成 routers/router.go（專案根目錄）：引入 router + schema，
// 以 Schema-first 串接 controller 與 model，作為專案路由的唯一入口。
func createRouterSetup(projectName, today string) error {
	content := "// Package routers 集中定義路由與中間件（Schema-first MVC 的 Router 層）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package routers\n\n" +
		"import (\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/router\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/schema\"\n\n" +
		"\t\"" + projectName + "/app/controllers\"\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		")\n\n" +
		"// Setup 註冊所有路由與中間件。\n" +
		"// 在 main.go 呼叫：routers.Setup(srv.Router())。\n" +
		"func Setup(r *router.Router) {\n" +
		"\t// Recovery / Logger / RejectEarlyData / Security / CORS 由 server.Start() 自動套用（v0.8.11+），\n" +
		"\t// 不要再 r.Use(middleware.DefaultMiddleware()...)，否則每個請求會跑兩次。\n" +
		"\t// 額外的全域中間件（CSRF、BodyLimit…）在此加\n\n" +
		"\t// 靜態資源\n" +
		"\tr.Static(\"/static\", \"./static\")\n\n" +
		"\t// Controller 實例（薄控制器；業務邏輯置於 app/services）\n" +
		"\thome := &controllers.HomeController{}\n\n" +
		// 先前首頁以 r.GET 註冊、沒有 Schema：全新專案的 hyp lint 即因 no_schema 失敗
		"\t// 首頁（回傳 HTML 檔案：沒有 JSON Output 型別，以 OutputName 標示內容型態，contract 測試不驗 body）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:     \"GET\",\n" +
		"\t\tPath:       \"/\",\n" +
		"\t\tSummary:    \"Welcome page\",\n" +
		"\t\tTags:       []string{\"web\"},\n" +
		"\t\tOutputName: \"text/html\",\n" +
		"\t}).Handle(home.Home)\n\n" +
		"\t// API（Schema-first：宣告 Input/Output 型別，供 c.BindInput 與 contract 測試對齊）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/api/health\",\n" +
		"\t\tSummary: \"Health check\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.HealthResp{},\n" +
		"\t}).Handle(home.Health)\n\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/api/info\",\n" +
		"\t\tSummary: \"Server info\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.InfoResp{},\n" +
		"\t}).Handle(home.Info)\n\n" +
		"\t// 在此註冊更多資源路由，例如由 `hyp generate controller user` 產生的：\n" +
		"\t// RegisterUserRoutes(r)\n" +
		"}\n"

	filename := filepath.Join(projectName, "routers", "router.go")
	return writeNewFile(filename, []byte(content), 0644)
}

// createGitKeep 在空目錄放 .gitkeep，讓該目錄能被 git 追蹤（hyp new / hyp api 共用）
func createGitKeep(dir string) error {
	return writeNewFile(filepath.Join(dir, ".gitkeep"), nil, 0644)
}

func createFullStackController(projectName, today string) error {
	controllerContent := "// Package controllers 承載 HTTP handler（薄控制器：解析輸入 → 呼叫 service → 寫回應）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package controllers\n\n" +
		"import (\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		")\n\n" +
		"// HomeController 處理首頁與系統狀態相關路由。\n" +
		"type HomeController struct{}\n\n" +
		"// Home 回傳歡迎頁面。\n" +
		"func (ctrl *HomeController) Home(c *hypcontext.Context) {\n" +
		"\tc.File(\"templates/welcome.html\")\n" +
		"}\n\n" +
		"// Health 回傳服務健康狀態。\n" +
		"func (ctrl *HomeController) Health(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.HealthResp{\n" +
		"\t\tSuccess: true,\n" +
		"\t\tMessage: \"Server is healthy\",\n" +
		"\t})\n" +
		"}\n\n" +
		"// Info 回傳伺服器資訊。\n" +
		"func (ctrl *HomeController) Info(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.InfoResp{\n" +
		"\t\tSuccess: true,\n" +
		"\t\tMessage: \"HypGo Framework\",\n" +
		"\t\tData: models.InfoDetail{\n" +
		"\t\t\tVersion:  \"0.8.6\",\n" +
		"\t\t\tProtocol: c.Request.Proto,\n" +
		"\t\t},\n" +
		"\t})\n" +
		"}\n"

	filename := filepath.Join(projectName, "app", "controllers", "home.go")
	return writeNewFile(filename, []byte(controllerContent), 0644)
}

func createWelcomeTemplate(projectName string) error {
	templateContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="container">
        <div class="hero">
            <h1 class="title">Welcome to HypGo</h1>
            <p class="subtitle">A Modern Go Web Framework with HTTP/3 Support</p>
            <div class="protocol-badge">{{.Protocol}}</div>
        </div>

        <div class="features">
            <div class="feature-card">
                <div class="feature-icon">🚀</div>
                <h3>HTTP/3 Ready</h3>
                <p>Built-in support for HTTP/3.0, HTTP/2.0, and HTTP/1.1</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">💾</div>
                <h3>Multiple Databases</h3>
                <p>MySQL, PostgreSQL, TiDB, Redis, and Cassandra support</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">⚡</div>
                <h3>WebSocket</h3>
                <p>Real-time communication with built-in WebSocket support</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">🔥</div>
                <h3>Hot Reload</h3>
                <p>Development mode with automatic code reloading</p>
            </div>
        </div>

        <div class="actions">
            <button id="testApi" class="btn btn-primary">Test API</button>
            <button id="connectWs" class="btn btn-secondary">Connect WebSocket</button>
        </div>

        <div id="output" class="output"></div>

        <div class="footer">
            <p>HypGo Framework &copy; 2024 | <a href="https://github.com/maoxiaoyue/hypgo">GitHub</a></p>
        </div>
    </div>

    <script src="/static/js/app.js"></script>
</body>
</html>
`

	filename := filepath.Join(projectName, "templates", "welcome.html")
	return writeNewFile(filename, []byte(templateContent), 0644)
}

func createStaticAssets(projectName string) error {
	// CSS 文件
	cssContent := `* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    min-height: 100vh;
    color: #333;
}

.container {
    max-width: 1200px;
    margin: 0 auto;
    padding: 2rem;
}

.hero {
    text-align: center;
    padding: 4rem 0;
    color: white;
}

.title {
    font-size: 4rem;
    font-weight: 700;
    margin-bottom: 1rem;
    text-shadow: 2px 2px 4px rgba(0,0,0,0.3);
}

.subtitle {
    font-size: 1.5rem;
    opacity: 0.9;
    margin-bottom: 2rem;
}

.protocol-badge {
    display: inline-block;
    background: rgba(255,255,255,0.2);
    padding: 0.5rem 1.5rem;
    border-radius: 25px;
    font-weight: 600;
    backdrop-filter: blur(10px);
}

.features {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
    gap: 2rem;
    margin: 3rem 0;
}

.feature-card {
    background: white;
    padding: 2rem;
    border-radius: 15px;
    box-shadow: 0 10px 30px rgba(0,0,0,0.1);
    text-align: center;
    transition: transform 0.3s ease;
}

.feature-card:hover {
    transform: translateY(-5px);
}

.feature-icon {
    font-size: 3rem;
    margin-bottom: 1rem;
}

.feature-card h3 {
    margin-bottom: 0.5rem;
    color: #667eea;
}

.actions {
    text-align: center;
    margin: 3rem 0;
}

.btn {
    padding: 0.75rem 2rem;
    font-size: 1rem;
    border: none;
    border-radius: 25px;
    cursor: pointer;
    margin: 0 0.5rem;
    transition: all 0.3s ease;
    font-weight: 600;
}

.btn-primary {
    background: #667eea;
    color: white;
}

.btn-primary:hover {
    background: #5a67d8;
    transform: scale(1.05);
}

.btn-secondary {
    background: #48bb78;
    color: white;
}

.btn-secondary:hover {
    background: #38a169;
    transform: scale(1.05);
}

.output {
    background: white;
    border-radius: 10px;
    padding: 1.5rem;
    margin: 2rem 0;
    min-height: 100px;
    box-shadow: 0 5px 20px rgba(0,0,0,0.1);
    display: none;
}

.output.show {
    display: block;
}

.output pre {
    background: #f7fafc;
    padding: 1rem;
    border-radius: 5px;
    overflow-x: auto;
}

.footer {
    text-align: center;
    padding: 2rem 0;
    color: white;
}

.footer a {
    color: white;
    text-decoration: underline;
}
`

	cssFile := filepath.Join(projectName, "static", "css", "style.css")
	if err := writeNewFile(cssFile, []byte(cssContent), 0644); err != nil {
		return err
	}

	// JavaScript 文件
	jsContent := `document.addEventListener('DOMContentLoaded', function() {
    const output = document.getElementById('output');
    const testApiBtn = document.getElementById('testApi');
    const connectWsBtn = document.getElementById('connectWs');
    
    let ws = null;

    // 測試 API
    testApiBtn.addEventListener('click', async function() {
        try {
            const response = await fetch('/api/info');
            const data = await response.json();
            
            output.classList.add('show');
            output.innerHTML = '<h3>API Response:</h3><pre>' + JSON.stringify(data, null, 2) + '</pre>';
        } catch (error) {
            output.classList.add('show');
            output.innerHTML = '<h3>Error:</h3><pre>' + error.message + '</pre>';
        }
    });

    // WebSocket 連接
    connectWsBtn.addEventListener('click', function() {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.close();
            connectWsBtn.textContent = 'Connect WebSocket';
            output.innerHTML += '<p>WebSocket disconnected</p>';
            return;
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(protocol + '//' + window.location.host + '/ws');

        ws.onopen = function() {
            connectWsBtn.textContent = 'Disconnect WebSocket';
            output.classList.add('show');
            output.innerHTML = '<h3>WebSocket Connected</h3>';
            
            // 發送測試消息
            ws.send(JSON.stringify({
                type: 'subscribe',
                data: { channel: 'test' }
            }));
        };

        ws.onmessage = function(event) {
            const data = JSON.parse(event.data);
            output.innerHTML += '<p>Received: ' + JSON.stringify(data) + '</p>';
        };

        ws.onerror = function(error) {
            output.innerHTML += '<p>Error: ' + error + '</p>';
        };

        ws.onclose = function() {
            connectWsBtn.textContent = 'Connect WebSocket';
            output.innerHTML += '<p>WebSocket closed</p>';
        };
    });
});
`

	jsFile := filepath.Join(projectName, "static", "js", "app.js")
	return writeNewFile(jsFile, []byte(jsContent), 0644)
}

func createGoMod(projectName string) error {
	// 版本取自 hyp 本身（version.go），postScaffoldUpgrade 再嘗試升到 @latest。
	// 先前以 git ls-remote git@github.com:...（SSH，沒有金鑰即失敗）查最新 tag，失敗時退回 v0.8.5——
	// 該版本沒有根 hypgo 套件，離線或 proxy 失敗時產生的專案永遠編不過
	content := fmt.Sprintf(`module %s

go 1.24

require github.com/maoxiaoyue/hypgo v%s
`, projectName, Version)

	filename := filepath.Join(projectName, "go.mod")
	return writeNewFile(filename, []byte(content), 0644)
}
