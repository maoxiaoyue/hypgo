package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/spf13/cobra"
)

var apiCmd = &cobra.Command{
	Use:   "api [project-name]",
	Short: "Create a new hypgo API-only project",
	Args:  cobra.ExactArgs(1),
	RunE:  runAPI,
}

func runAPI(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// 創建 API 項目目錄結構（不包含 static 和 templates）
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "app", "middleware"),
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, "logs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// 創建配置文件
	if err := createAPIConfigFile(projectName); err != nil {
		return err
	}

	// 創建主程序文件
	if err := createAPIMainFile(projectName); err != nil {
		return err
	}

	// 創建 API 控制器
	if err := createAPIController(projectName); err != nil {
		return err
	}

	// 創建中間件
	if err := createMiddleware(projectName); err != nil {
		return err
	}

	// 創建 go.mod
	if err := createGoMod(projectName); err != nil {
		return err
	}

	fmt.Printf("✨ Successfully created API project: %s\n", projectName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   ├── services/\n")
	fmt.Printf("   │   └── middleware/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml\n")
	fmt.Printf("   ├── logs/\n")
	fmt.Printf("   ├── go.mod\n")
	fmt.Printf("   └── main.go\n")
	fmt.Printf("\n🚀 Get started:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   hyp run\n")

	return nil
}

func createAPIConfigFile(projectName string) error {
	configContent := `# hypgo API Configuration

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
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

database:
  driver: mysql  # mysql, postgres, tidb, redis, cassandra
  dsn: "user:password@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local"
  max_idle_conns: 10
  max_open_conns: 100
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0
  cassandra:
    hosts:
      - "localhost:9042"
    keyspace: "hypgo"

logger:
  level: debug  # debug, info, notice, warning, emergency
  output: logs/api.log  # stdout or file path
  colors: true
  rotation:
    max_size: 100MB
    max_age: 7d
    max_backups: 10
    compress: true

rabbitmq:
  url: "amqp://guest:guest@localhost:5672/"
  exchange: "hypgo"
  queue: "api"

# API 特定配置
api:
  version: "v1"
  rate_limit:
    enabled: true
    requests_per_minute: 60
  cors:
    enabled: true
    allowed_origins:
      - "*"
    allowed_methods:
      - GET
      - POST
      - PUT
      - DELETE
      - OPTIONS
    allowed_headers:
      - Content-Type
      - Authorization
  jwt:
    secret: "your-secret-key"
    expiration: 24h
`

	filename := filepath.Join(projectName, "config", "config.yaml")
	return os.WriteFile(filename, []byte(configContent), 0644)
}

func createAPIMainFile(projectName string) error {
	mainContent := `package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/yourusername/hypgo/pkg/config"
    "github.com/yourusername/hypgo/pkg/database"
    "github.com/yourusername/hypgo/pkg/logger"
    "github.com/yourusername/hypgo/pkg/server"
    "{{.ProjectName}}/app/controllers"
    "{{.ProjectName}}/app/middleware"
)

func main() {
    // 載入配置
    cfg, err := config.Load("config/config.yaml")
    if err != nil {
        log.Fatal("Failed to load config:", err)
    }

    // 初始化日誌
    log, err := logger.New(
        cfg.Logger.Level,
        cfg.Logger.Output,
        &cfg.Logger.Rotation,
        cfg.Logger.Colors,
    )
    if err != nil {
        log.Fatal("Failed to initialize logger:", err)
    }
    defer log.Close()

    // 初始化數據庫
    db, err := database.New(&cfg.Database)
    if err != nil {
        log.Emergency("Failed to initialize database: %v", err)
        os.Exit(1)
    }
    defer db.Close()

    // 創建服務器
    srv := server.New(cfg, log)
    
    // 設置全局中間件
    router := srv.Router()
    router.
