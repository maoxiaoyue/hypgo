package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
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
			return runNewDesktop(projectName)
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
  │   ├── services/      Business logic + Error Catalog
  │   └── config/        config.yaml
  ├── main.go            Entry point
  └── go.mod

After creation:
  cd mytool && go mod tidy && go run .

Add subcommands:
  hyp generate command process
  hyp generate command export`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
  │   ├── services/      Business logic + Error Catalog
  │   └── config/
  │       └── config.yaml
  ├── main.go            Entry point (Fyne app + window)
  └── go.mod

After creation:
  cd myapp && go mod tidy && go run .

Add views:
  hyp generate view settings
  hyp generate view dashboard

Requires: C compiler (gcc) for CGO (Fyne dependency)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNewDesktop(args[0])
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
  │   ├── services/          Business logic + Error Catalog
  │   └── config/
  │       └── config.yaml
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
		return runNewGRPC(args[0])
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newCLICmd)
	newCmd.AddCommand(newDesktopCmd)
	newCmd.AddCommand(newGRPCCmd)
}

// runNewCLI 生成 CLI 專案
func runNewCLI(projectName string) error {
	if err := scaffold.GenerateCLIProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create CLI project: %w", err)
	}

	fmt.Printf("\n✨ Successfully created CLI project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── commands/\n")
	fmt.Printf("   │   │   └── root.go\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   ├── services/\n")
	fmt.Printf("   │   └── config/\n")
	fmt.Printf("   │       └── config.yaml\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add subcommands:\n")
	fmt.Printf("   hyp generate command process\n")
	fmt.Printf("   hyp generate command export\n")

	return nil
}

// runNewDesktop 生成 Desktop 專案（Fyne）
func runNewDesktop(projectName string) error {
	if err := scaffold.GenerateDesktopProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create desktop project: %w", err)
	}

	fmt.Printf("\n✨ Successfully created Desktop project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── views/\n")
	fmt.Printf("   │   │   └── main_view.go\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   ├── services/\n")
	fmt.Printf("   │   └── config/\n")
	fmt.Printf("   │       └── config.yaml\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add views:\n")
	fmt.Printf("   hyp generate view settings\n")
	fmt.Printf("   hyp generate view dashboard\n")
	fmt.Printf("\n⚠️  Requires: C compiler (gcc) for CGO (Fyne dependency)\n")

	return nil
}

// runNewGRPC 生成 gRPC 微服務專案
func runNewGRPC(projectName string) error {
	if err := scaffold.GenerateGRPCProject(projectName, projectName, projectName); err != nil {
		return fmt.Errorf("failed to create gRPC project: %w", err)
	}

	lowerName := strings.ToLower(projectName)
	fmt.Printf("\n✨ Successfully created gRPC project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── proto/%spb/\n", lowerName)
	fmt.Printf("   │   │   └── %s.proto\n", lowerName)
	fmt.Printf("   │   ├── rpc/\n")
	fmt.Printf("   │   │   └── %s_server.go\n", lowerName)
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   ├── services/\n")
	fmt.Printf("   │   └── config/\n")
	fmt.Printf("   │       └── config.yaml\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   ├── Makefile\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   make proto              # compile .proto → Go code\n")
	fmt.Printf("   go run .\n")
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
		filepath.Join(projectName, "config"),
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

	// 創建配置文件
	if err := createNewConfigFile(projectName); err != nil {
		return err
	}

	// 創建 LLM 配置文件
	if err := createLLMConfigFile(filepath.Join(projectName, "config")); err != nil {
		return err
	}

	// 創建主程序文件
	if err := createMainFile(projectName); err != nil {
		return err
	}

	// 創建示例控制器
	if err := createFullStackController(projectName); err != nil {
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

	fmt.Printf("✨ Successfully created full-stack project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml\n")
	fmt.Printf("   ├── logs/\n")
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
	return os.WriteFile(filename, []byte(configContent), 0644)
}

// createLLMConfigFile 寫入 llm.yaml 到指定目錄。
// 內容來自 scaffold.LLMYamlTemplate，預設 mode=none（不啟用 LLM）。
func createLLMConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "llm.yaml")
	return os.WriteFile(filename, []byte(scaffold.LLMYamlTemplate), 0644)
}

func createMainFile(projectName string) error {
	mainContent := `package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/maoxiaoyue/hypgo/pkg/config"
    hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
    "github.com/maoxiaoyue/hypgo/pkg/schema"
    "github.com/maoxiaoyue/hypgo/pkg/server"
)

func main() {
    // 載入配置
    cfg := &config.Config{}
    loader := config.NewConfigLoader("config/config.yaml")
    if err := loader.Load("config/config.yaml", cfg); err != nil {
        log.Fatal("Failed to load config:", err)
    }
    cfg.ApplyDefaults()

    // 初始化日誌
    appLog := logger.NewLogger()
    defer appLog.Close()

    // 創建服務器
    srv := server.New(cfg, appLog)
    r := srv.Router()

    // 靜態檔案
    r.Static("/static", "./static")

    // 首頁
    r.GET("/", func(c *hypcontext.Context) {
        c.File("templates/welcome.html")
    })

    // API 路由（Schema-first）
    r.Schema(schema.Route{
        Method:  "GET",
        Path:    "/api/health",
        Summary: "Health check",
    }).Handle(func(c *hypcontext.Context) {
        c.JSON(200, map[string]interface{}{
            "success": true,
            "message": "Server is healthy",
        })
    })

    r.Schema(schema.Route{
        Method:  "GET",
        Path:    "/api/info",
        Summary: "Server info",
    }).Handle(func(c *hypcontext.Context) {
        c.JSON(200, map[string]interface{}{
            "success": true,
            "message": "HypGo Framework",
            "data": map[string]interface{}{
                "version":  "0.8.5",
                "protocol": c.Request.Proto,
            },
        })
    })

    // 啟動服務器
    go func() {
        appLog.Info("Starting HypGo server on %s", cfg.Server.Addr)
        if err := srv.Start(); err != nil {
            appLog.Error("Server error: %v", err)
            os.Exit(1)
        }
    }()

    // 優雅關閉
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    appLog.Info("Shutting down server...")
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := srv.Shutdown(ctx); err != nil {
        appLog.Error("Server forced to shutdown: %v", err)
    }
    appLog.Info("Server exited")
}
`

	filename := filepath.Join(projectName, "main.go")
	return os.WriteFile(filename, []byte(mainContent), 0644)
}

func createFullStackController(projectName string) error {
	controllerContent := `package controllers

import (
    hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// HomeController 處理首頁相關路由
type HomeController struct{}

func (ctrl *HomeController) Home(c *hypcontext.Context) {
    c.File("templates/welcome.html")
}

func (ctrl *HomeController) Health(c *hypcontext.Context) {
    c.JSON(200, map[string]interface{}{
        "success": true,
        "message": "Server is healthy",
    })
}

func (ctrl *HomeController) Info(c *hypcontext.Context) {
    c.JSON(200, map[string]interface{}{
        "success": true,
        "message": "HypGo Framework",
        "data": map[string]interface{}{
            "version":  "0.8.5",
            "protocol": c.Request.Proto,
        },
    })
}
`

	filename := filepath.Join(projectName, "app", "controllers", "home.go")
	return os.WriteFile(filename, []byte(controllerContent), 0644)
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
	return os.WriteFile(filename, []byte(templateContent), 0644)
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
	if err := os.WriteFile(cssFile, []byte(cssContent), 0644); err != nil {
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
	return os.WriteFile(jsFile, []byte(jsContent), 0644)
}

func createGoMod(projectName string) error {
	latestTag, err := getLatestGitTag("github.com/maoxiaoyue/hypgo")
	if err != nil {
		// 如果無法獲取標籤，使用目前版本
		latestTag = "v0.8.5"
		fmt.Fprintf(os.Stderr, "Warning: Failed to get latest tag, using %s: %v\n", latestTag, err)
	}

	content := fmt.Sprintf(`module %s

go 1.24

require github.com/maoxiaoyue/hypgo %s
`, projectName, latestTag)

	filename := filepath.Join(projectName, "go.mod")
	return os.WriteFile(filename, []byte(content), 0644)
}

// getLatestGitTag 獲取指定儲存庫的最新標籤
func getLatestGitTag(repo string) (string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", fmt.Sprintf("git@%s.git", repo))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run git ls-remote: %w", err)
	}

	// 解析 git ls-remote 字串，提取標籤
	tags := []string{}
	tagRegex := regexp.MustCompile(`refs/tags/(.+)$`)
	for _, line := range strings.Split(string(output), "\n") {
		if matches := tagRegex.FindStringSubmatch(line); len(matches) > 1 {
			tag := strings.TrimSuffix(matches[1], "^{}") // 移除 ^{} 後綴
			if isValidSemver(tag) {
				tags = append(tags, tag)
			}
		}
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no valid semantic version tags found")
	}

	// 按語義版本排序，選擇最新版本
	sort.Slice(tags, func(i, j int) bool {
		return compareSemver(tags[i], tags[j]) > 0
	})

	return tags[0], nil
}

// isValidSemver 簡單檢查是否為語義版本（vX.Y.Z）
func isValidSemver(tag string) bool {
	semverRegex := regexp.MustCompile(`^v\d+\.\d+\.\d+(-.*)?$`)
	return semverRegex.MatchString(tag)
}

// compareSemver 比較兩個語義版本
func compareSemver(v1, v2 string) int {
	// 簡單實現：移除 "v" 前綴並比較
	v1Parts := strings.Split(strings.TrimPrefix(v1, "v"), ".")
	v2Parts := strings.Split(strings.TrimPrefix(v2, "v"), ".")

	for i := 0; i < len(v1Parts) && i < len(v2Parts); i++ {
		v1Num, _ := strconv.Atoi(strings.Split(v1Parts[i], "-")[0])
		v2Num, _ := strconv.Atoi(strings.Split(v2Parts[i], "-")[0])
		if v1Num != v2Num {
			return v1Num - v2Num
		}
	}
	return len(v1Parts) - len(v2Parts)
}
