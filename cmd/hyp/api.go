// @chris
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
)

// llmYamlContent 為 hyp api 生成 .hyp/llm.yaml 時採用的預設內容。
// 直接複用 scaffold 套件的共用模板，確保所有專案模式內容一致。
var llmYamlContent = scaffold.LLMYamlTemplate

// commentYamlContent 為 hyp api 生成 .hyp/comment.yaml 時採用的預設內容。
var commentYamlContent = scaffold.CommentYamlTemplate

// hypConfigYamlContent 為 hyp api 生成 .hyp/config.yaml 時採用的預設內容。
var hypConfigYamlContent = scaffold.HypConfigYamlTemplate

var apiCmd = &cobra.Command{
	Use:   "api [project-name]",
	Short: "Create a new HypGo API-only project with HTTP/3 support",
	Args:  cobra.ExactArgs(1),
	RunE:  runAPI,
}

func init() {
	rootCmd.AddCommand(apiCmd)
}

func runAPI(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// 創建 API 項目目錄結構
	// 與 hyp new 同構（app/{controllers,models,services} + routers/），
	// 不再產生自製的 internal/{logger,database,cache}：框架的 pkg/logger、
	// pkg/hidb 已涵蓋，重造一份只會與框架 API 漂移（舊版即因此編不過）
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "routers"), // 路由層置於專案根目錄（與 app/ 平行）
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, ".hyp"),
		filepath.Join(projectName, "migrations"),
		filepath.Join(projectName, "tests"),
		filepath.Join(projectName, "docs"),
		filepath.Join(projectName, "logs"),
		filepath.Join(projectName, "certs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// logs/ 為空目錄，放 .gitkeep 讓 clone 後仍存在（.gitignore 已排除 logs/* 但保留 .gitkeep）
	if err := createGitKeep(filepath.Join(projectName, "logs")); err != nil {
		return err
	}

	// 創建所有非 Go 檔案（設定、部署、遷移）
	files := []fileTemplate{
		{Path: "config/config.yaml", Content: configYamlContent},
		{Path: ".hyp/llm.yaml", Content: llmYamlContent},
		{Path: ".hyp/comment.yaml", Content: commentYamlContent},
		{Path: ".hyp/config.yaml", Content: hypConfigYamlContent},
		{Path: ".env.example", Content: envExampleContent},

		// 部署和配置
		{Path: "Dockerfile", Content: dockerfileContent},
		{Path: "docker-compose.yml", Content: dockerComposeContent},
		{Path: "Makefile", Content: makefileContent},
		{Path: ".gitignore", Content: gitignoreContent},
		{Path: ".air.toml", Content: airTomlContent},
		{Path: "README.md", Content: readmeContent},
		{Path: "go.mod", Content: goModContent},

		// 數據庫遷移
		{Path: "migrations/001_create_users.up.sql", Content: createUsersUpSQL},
		{Path: "migrations/001_create_users.down.sql", Content: createUsersDownSQL},
		{Path: "migrations/002_create_roles.up.sql", Content: createRolesUpSQL},
		{Path: "migrations/002_create_roles.down.sql", Content: createRolesDownSQL},
	}

	// 模板數據
	data := map[string]string{
		"ProjectName": projectName,
	}

	// 創建所有檔案
	for _, file := range files {
		fullPath := filepath.Join(projectName, file.Path)
		if err := createTemplateFile(fullPath, file.Content, data); err != nil {
			return fmt.Errorf("failed to create %s: %w", file.Path, err)
		}
	}

	today := time.Now().Format("2006-01-02")

	// Go 程式碼一律走 pkg/scaffold 的生成器（與 hyp generate 同一套、有測試覆蓋），
	// 保證產出對齊框架真實 API；user 資源 = model + service + controller + Schema 路由
	const resource = "user"
	gen := []struct {
		label string
		fn    func() error
	}{
		{"app/models/user.go", func() error { return scaffold.GenerateModel(filepath.Join(projectName, "app", "models"), resource) }},
		{"app/services/user_service.go", func() error { return scaffold.GenerateService(filepath.Join(projectName, "app", "services"), resource) }},
		{"app/controllers/user_controller.go", func() error {
			return scaffold.GenerateController(filepath.Join(projectName, "app", "controllers"), resource, projectName)
		}},
		{"routers/user.go", func() error {
			return scaffold.GenerateRouter(filepath.Join(projectName, "routers"), resource, projectName)
		}},
		{"routers/middleware.go", func() error { return scaffold.GenerateMiddleware(filepath.Join(projectName, "routers")) }},
		{"app/models/health.go + app/controllers/health.go", func() error { return createAPIHealth(projectName, today) }},
		{"routers/router.go", func() error { return createAPIRouterSetup(projectName, today) }},
		{"main.go", func() error { return createAPIMainFile(projectName) }},
	}
	for _, g := range gen {
		if err := g.fn(); err != nil {
			return fmt.Errorf("failed to create %s: %w", g.label, err)
		}
	}

	// 把生成專案的 hypgo 依賴升到 @latest（如果可以）
	postScaffoldUpgrade(projectName)

	// 打印成功信息
	printSuccessMessage(projectName)

	return nil
}

// createAPIRouterSetup 生成 routers/router.go（專案根目錄）：全域中間件 +
// /health + RegisterUserRoutes（由 scaffold.GenerateRouter 產生），
// 作為 API 專案路由的唯一入口；main.go 以 routers.Setup(...) 呼叫。
func createAPIRouterSetup(projectName, today string) error {
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
		"// 在 main.go 呼叫：routers.Setup(app.Server().Router())。\n" +
		"func Setup(r *router.Router) {\n" +
		"\t// Recovery / Logger / Security / CORS 由 server.Start() 自動套用（v0.8.11+），\n" +
		"\t// 不要再 r.Use(middleware.DefaultMiddleware()...)，否則每個請求會跑兩次。\n" +
		"\t// 額外的全域中間件（JWT、RateLimiter、BodyLimit…）在此加，或用 routers/middleware.go 的 APIMiddleware()\n\n" +
		"\t// 健康檢查（Schema-first：宣告 Output 供 contract 測試與 manifest 使用）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/health\",\n" +
		"\t\tSummary: \"Health check\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.HealthResp{},\n" +
		"\t}).Handle(controllers.HealthCheck)\n\n" +
		"\t// 資源路由（各資源一檔，由 hyp generate controller <name> 產生後在此註冊）\n" +
		"\tRegisterUserRoutes(r)\n" +
		"}\n"

	filename := filepath.Join(projectName, "routers", "router.go")
	return os.WriteFile(filename, []byte(content), 0644)
}

// createAPIHealth 生成健康檢查的 Schema Output DTO 與 handler
func createAPIHealth(projectName, today string) error {
	model := "// Package models 定義資料模型與 Schema 的 Request/Response DTO。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package models\n\n" +
		"// HealthResp 健康檢查回應（Schema Output）。\n" +
		"type HealthResp struct {\n" +
		"\tStatus    string `json:\"status\"`\n" +
		"\tProtocol  string `json:\"protocol\"`\n" +
		"\tTimestamp int64  `json:\"timestamp\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(projectName, "app", "models", "health.go"), []byte(model), 0644); err != nil {
		return err
	}

	ctrl := "// Package controllers 承載 HTTP handler（薄控制器：解析輸入 → 呼叫 service → 寫回應）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package controllers\n\n" +
		"import (\n" +
		"\t\"time\"\n\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		")\n\n" +
		"// HealthCheck 回報服務狀態與目前連線協議（HTTP/1.1、HTTP/2 或 HTTP/3）。\n" +
		"func HealthCheck(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.HealthResp{\n" +
		"\t\tStatus:    \"ok\",\n" +
		"\t\tProtocol:  c.Protocol(),\n" +
		"\t\tTimestamp: time.Now().Unix(),\n" +
		"\t})\n" +
		"}\n"
	return os.WriteFile(filepath.Join(projectName, "app", "controllers", "health.go"), []byte(ctrl), 0644)
}

// createAPIMainFile 生成 main.go：以根 hypgo facade 一行式啟動，
// 可選初始化 hidb（config 的 database.dsn 為空時略過），路由交給 routers.Setup。
func createAPIMainFile(projectName string) error {
	content := "package main\n\n" +
		"import (\n" +
		"\t\"os\"\n\n" +
		"\t\"github.com/maoxiaoyue/hypgo\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/config\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb/mysql\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb/pg\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/logger\"\n\n" +
		"\t\"" + projectName + "/routers\"\n" +
		")\n\n" +
		"func main() {\n" +
		"\t// 一行式啟動：載入 config/config.yaml（找不到時用預設值）、建立 logger 與 server\n" +
		"\tapp := hypgo.New(hypgo.WithConfigPath(\"config/config.yaml\"))\n" +
		"\tlog := app.Logger()\n\n" +
		"\t// 資料庫（database.dsn 留空則略過）；service 透過建構子注入：\n" +
		"\t//   services.NewUserService(db, log)\n" +
		"\tif db := openDatabase(app.Config(), log); db != nil {\n" +
		"\t\tdefer db.Close()\n" +
		"\t}\n\n" +
		"\t// 所有路由與中間件定義於 routers/router.go\n" +
		"\trouters.Setup(app.Server().Router())\n\n" +
		"\t// Run 會阻塞到收到 SIGINT/SIGTERM 並完成優雅關閉\n" +
		"\tif err := app.Run(); err != nil {\n" +
		"\t\tlog.Errorf(\"server error: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"}\n\n" +
		"// openDatabase 依 config 的 database 區段建立 hidb 連線；dsn 為空回傳 nil\n" +
		"func openDatabase(cfg *config.Config, log *logger.Logger) *hidb.Database {\n" +
		"\tif cfg.Database.DSN == \"\" {\n" +
		"\t\tlog.Info(\"database.dsn not set, skipping database init\")\n" +
		"\t\treturn nil\n" +
		"\t}\n\n" +
		"\tvar dialect hidb.Dialect\n" +
		"\tswitch cfg.Database.Driver {\n" +
		"\tcase \"mysql\", \"tidb\":\n" +
		"\t\tdialect = mysql.New()\n" +
		"\tdefault:\n" +
		"\t\tdialect = pg.New()\n" +
		"\t}\n\n" +
		"\tdb, err := hidb.NewWithInterface(&cfg.Database, hidb.WithDialect(dialect))\n" +
		"\tif err != nil {\n" +
		"\t\tlog.Errorf(\"database init failed: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"\treturn db\n" +
		"}\n"

	return os.WriteFile(filepath.Join(projectName, "main.go"), []byte(content), 0644)
}

type fileTemplate struct {
	Path    string
	Content string
}

func createTemplateFile(filepath, content string, data interface{}) error {
	tmpl, err := template.New("file").Parse(content)
	if err != nil {
		return err
	}

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

func printSuccessMessage(projectName string) {
	fmt.Printf("\n✨ Successfully created HypGo API project: %s\n\n", projectName)
	fmt.Printf("📁 Project Structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/    # user_controller.go, health.go（薄控制器）\n")
	fmt.Printf("   │   ├── models/         # user.go, health.go（Bun model + Schema DTO）\n")
	fmt.Printf("   │   └── services/       # user_service.go（業務邏輯 + Error Catalog）\n")
	fmt.Printf("   ├── routers/            # router.go（Setup）+ user.go（Schema 路由）+ middleware.go\n")
	fmt.Printf("   ├── config/config.yaml  # server / database / logger\n")
	fmt.Printf("   ├── .hyp/               # llm.yaml / comment.yaml / config.yaml\n")
	fmt.Printf("   ├── migrations/         # SQL migrations\n")
	fmt.Printf("   ├── logs/               # .gitkeep（logger 輸出 logs/api.log）\n")
	fmt.Printf("   ├── certs/              # make cert → HTTP/3 用 TLS 憑證\n")
	fmt.Printf("   ├── Dockerfile / docker-compose.yml / Makefile\n")
	fmt.Printf("   └── main.go             # hypgo.New() 一行式啟動 + routers.Setup\n")
	fmt.Printf("\n🚀 Quick Start:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go run .                # http://localhost:8080/health\n")
	fmt.Printf("   make cert               # 產生自簽憑證後，config.yaml 開 tls.enabled 即得 HTTP/3\n")
	fmt.Printf("   hyp generate controller order   # 新增資源 → 在 routers/router.go 註冊\n")
	fmt.Printf("   hyp lint --deep         # CI gate：Schema 完整度 + handler 型別對齊\n")
	fmt.Printf("\n")
}

// ===== File Contents =====

// configYamlContent 對齊 pkg/config.Config 的真實欄位（server / database / logger）
const configYamlContent = `# HypGo API Configuration（欄位對應 pkg/config.Config）

server:
  protocol: auto           # http1 | http2 | http3 | auto（auto = TCP 上 H1/H2，tls 啟用時同時起 H3）
  addr: :8080
  read_timeout: 30         # 秒
  write_timeout: 30
  idle_timeout: 120
  max_handlers: 1000
  max_concurrent_streams: 250
  max_read_frame_size: 1048576
  enable_graceful_restart: true
  # 可信代理網段：服務跑在 nginx / LB 之後時務必設定，否則 c.ClientIP()
  # 只回傳實際連線來源（安全預設），不會解析 X-Forwarded-For / X-Real-IP，
  # IPWhitelist / RateLimiter 等以 ClientIP 為 key 的功能會全部失準。
  # 支援 CIDR 或單一 IP。
  trusted_proxies:
    - 127.0.0.1
    - 10.0.0.0/8
  tls:
    enabled: false         # make cert 產生自簽憑證後改 true（HTTP/3 必須 TLS）
    cert_file: "certs/server.crt"
    key_file: "certs/server.key"

database:
  driver: postgres         # postgres | mysql | tidb（main.go 依此選 dialect）
  dsn: ""                  # 留空 = 不連資料庫；例：postgres://user:pass@localhost:5432/app?sslmode=disable
  max_idle_conns: 10
  max_open_conns: 100
  redis:                   # 需要時在 main.go 加上 hidb.WithRedis(redis.New())
    addr: "localhost:6379"
    password: ""
    db: 0

logger:
  level: info              # debug | info | notice | warning | emergency
  output: "logs/api.log"   # stdout 或檔案路徑
  colors: true
  rotation:
    max_size: 100MB
    max_age: 7d
    max_backups: 10
    compress: true
`

const envExampleContent = `# 供 docker-compose.yml 與 Makefile 的 migrate 目標使用；
# 應用程式本身讀 config/config.yaml（不展開環境變數）
DB_DSN=postgres://hypgo:password@localhost:5432/hypgo_db?sslmode=disable
`

const dockerfileContent = `# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

ENV TZ=Asia/Taipei

RUN addgroup -g 1000 -S appuser && \
    adduser -u 1000 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/config ./config

RUN mkdir -p logs certs && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080 8443

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./main"]`

const dockerComposeContent = `version: '3.8'

services:
  api:
    build: .
    container_name: hypgo-api
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8443:8443"
    environment:
      - ENV=development
      - DB_DSN=postgres://hypgo:password@postgres:5432/hypgo_db?sslmode=disable
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=
      - JWT_SECRET=your-secret-key-change-this-in-production
    volumes:
      - ./config:/app/config:ro
      - ./logs:/app/logs
      - ./certs:/app/certs:ro
    depends_on:
      - postgres
      - redis
    networks:
      - hypgo-network

  postgres:
    image: postgres:15-alpine
    container_name: hypgo-postgres
    restart: unless-stopped
    environment:
      - POSTGRES_USER=hypgo
      - POSTGRES_PASSWORD=password
      - POSTGRES_DB=hypgo_db
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "5432:5432"
    networks:
      - hypgo-network

  redis:
    image: redis:7-alpine
    container_name: hypgo-redis
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
    ports:
      - "6379:6379"
    networks:
      - hypgo-network

volumes:
  postgres-data:
  redis-data:

networks:
  hypgo-network:
    driver: bridge`

const makefileContent = `.PHONY: help build run test clean docker migrate dev

# Variables
APP_NAME=hypgo-api
MAIN_PATH=.
DOCKER_IMAGE=$(APP_NAME):latest

# Colors
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[0;33m
NC=\033[0m

help: ## Show help
	@echo '$(GREEN)Usage:$(NC)'
	@echo '  make [target]'
	@echo ''
	@echo '$(GREEN)Targets:$(NC)'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the application
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	@go build -o bin/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)Build complete!$(NC)"

run: ## Run the application
	@echo "$(GREEN)Running $(APP_NAME)...$(NC)"
	@go run $(MAIN_PATH)

dev: ## Run with hot reload
	@echo "$(GREEN)Running in development mode...$(NC)"
	@air

test: ## Run tests
	@echo "$(GREEN)Running tests...$(NC)"
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Tests complete!$(NC)"

lint: ## Run linter
	@echo "$(GREEN)Running linter...$(NC)"
	@golangci-lint run ./...

fmt: ## Format code
	@echo "$(GREEN)Formatting code...$(NC)"
	@go fmt ./...
	@goimports -w .

clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf bin/ coverage.* tmp/
	@echo "$(GREEN)Clean complete!$(NC)"

docker: ## Build Docker image
	@echo "$(GREEN)Building Docker image...$(NC)"
	@docker build -t $(DOCKER_IMAGE) .

docker-run: docker ## Run Docker container
	@echo "$(GREEN)Running Docker container...$(NC)"
	@docker run -p 8080:8080 -p 8443:8443 $(DOCKER_IMAGE)

docker-compose-up: ## Start all services
	@echo "$(GREEN)Starting services...$(NC)"
	@docker-compose up -d

docker-compose-down: ## Stop all services
	@echo "$(YELLOW)Stopping services...$(NC)"
	@docker-compose down

migrate: ## Run database migrations
	@echo "$(GREEN)Running migrations...$(NC)"
	@migrate -path migrations -database "$${DB_DSN}" up

migrate-down: ## Rollback migrations
	@echo "$(YELLOW)Rolling back migrations...$(NC)"
	@migrate -path migrations -database "$${DB_DSN}" down 1

migrate-create: ## Create new migration
	@echo "$(GREEN)Creating migration: $(name)$(NC)"
	@migrate create -ext sql -dir migrations -seq $(name)

seed: ## Seed the database
	@echo "$(GREEN)Seeding database...$(NC)"
	@go run scripts/seed.go

docs: ## Generate API documentation
	@echo "$(GREEN)Generating documentation...$(NC)"
	@swag init -g main.go -o docs

install-tools: ## Install development tools
	@echo "$(GREEN)Installing development tools...$(NC)"
	@go install github.com/cosmtrek/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "$(GREEN)Tools installed!$(NC)"

cert: ## Generate self-signed certificates
	@echo "$(GREEN)Generating certificates...$(NC)"
	@mkdir -p certs
	@openssl req -x509 -newkey rsa:4096 -keyout certs/server.key -out certs/server.crt -days 365 -nodes -subj "/CN=localhost"
	@echo "$(GREEN)Certificates generated!$(NC)"

.DEFAULT_GOAL := help`

const gitignoreContent = `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
bin/

# Test binary
*.test

# Output
*.out
coverage.html

# Dependency directories
vendor/

# Go workspace
go.work

# Environment variables
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/
*.swp
*.swo
*~
.DS_Store

# Logs（保留 logs/.gitkeep 讓目錄存在）
logs/*
!logs/.gitkeep
*.log

# Certificates
certs/
*.pem
*.key
*.crt

# Database
*.db
*.sqlite
*.sqlite3

# Temporary files
tmp/
temp/

# Build artifacts
dist/
build/

# Air config
.air.tmp/`

const airTomlContent = `root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  args_bin = []
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ."
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "testdata", "docs", "scripts", "logs", "certs"]
  exclude_file = []
  exclude_regex = ["_test.go"]
  exclude_unchanged = false
  follow_symlink = false
  full_bin = ""
  include_dir = []
  include_ext = ["go", "tpl", "tmpl", "html"]
  include_file = []
  kill_delay = "0s"
  log = "build-errors.log"
  poll = false
  poll_interval = 0
  rerun = false
  rerun_delay = 500
  send_interrupt = false
  stop_on_error = false

[color]
  app = ""
  build = "yellow"
  main = "magenta"
  runner = "green"
  watcher = "cyan"

[log]
  main_only = false
  time = false

[misc]
  clean_on_exit = false

[screen]
  clear_on_rebuild = false
  keep_scroll = true`

const readmeContent = `# {{.ProjectName}}

API server built with the HypGo framework — Schema-first routes, HTTP/1.1 + HTTP/2 + HTTP/3 (QUIC).

## Quick Start

` + "```bash" + `
go mod tidy
go run .                 # http://localhost:8080/health
` + "```" + `

HTTP/3 needs TLS: run ` + "`make cert`" + ` (self-signed cert into ` + "`certs/`" + `), then set ` + "`server.tls.enabled: true`" + ` in ` + "`config/config.yaml`" + `.
` + "`server.protocol: auto`" + ` serves HTTP/1.1 + HTTP/2 over TCP and, once TLS is on, HTTP/3 over UDP as well.

## Endpoints

| Method | Path | Handler |
|--------|------|---------|
| GET | ` + "`/health`" + ` | ` + "`controllers.HealthCheck`" + ` |
| GET | ` + "`/api/user`" + ` | ` + "`UserController.List`" + ` |
| POST | ` + "`/api/user`" + ` | ` + "`UserController.Create`" + ` (` + "`c.BindInput`" + ` → validate → 201) |
| GET | ` + "`/api/user/:id`" + ` | ` + "`UserController.Get`" + ` |
| PUT | ` + "`/api/user/:id`" + ` | ` + "`UserController.Update`" + ` |
| DELETE | ` + "`/api/user/:id`" + ` | ` + "`UserController.Delete`" + ` |

Every route is registered with ` + "`r.Schema(...)`" + ` (Input/Output types), so ` + "`hyp lint --deep`" + `,
` + "`contract.TestAll`" + ` and ` + "`.hyp/context.yaml`" + ` all understand the API without extra work.

## Adding a resource

` + "```bash" + `
hyp generate controller order   # app/controllers/order_controller.go + routers/order.go
hyp generate model order        # app/models/order.go (Bun model + Req/Resp DTOs)
hyp generate service order      # app/services/order_service.go
` + "```" + `

Then add ` + "`RegisterOrderRoutes(r)`" + ` to ` + "`routers/router.go`" + `.

## Database

Set ` + "`database.dsn`" + ` in ` + "`config/config.yaml`" + ` (` + "`driver`" + `: postgres / mysql / tidb). ` + "`main.go`" + ` opens
` + "`hidb`" + ` with the matching dialect and skips it when ` + "`dsn`" + ` is empty. Inject it into services:

` + "```go" + `
svc := services.NewUserService(db, log)
` + "```" + `

Redis: add ` + "`hidb.WithRedis(redis.New())`" + ` (import ` + "`github.com/maoxiaoyue/hypgo/pkg/hidb/redis`" + `) next to ` + "`WithDialect`" + `.

## Project Structure

` + "```" + `
.
├── app/
│   ├── controllers/    # thin handlers (parse → service → respond)
│   ├── models/         # Bun models + Schema Req/Resp DTOs
│   └── services/       # business logic + Error Catalog
├── routers/            # router.go (Setup) + <resource>.go Schema routes + middleware.go
├── config/config.yaml  # server / database / logger
├── .hyp/               # llm.yaml / comment.yaml / config.yaml (AI tooling)
├── migrations/         # SQL migrations (make migrate)
├── logs/               # logger output (logs/api.log)
├── certs/              # make cert
└── main.go             # hypgo.New() one-line startup + routers.Setup
` + "```" + `

## Tooling

` + "```bash" + `
make dev          # hot reload (air)
make test         # go test -race
make cert         # self-signed TLS cert for HTTP/3
make migrate      # golang-migrate (needs DB_DSN, see .env.example)
hyp lint --deep   # Schema completeness + handler type alignment (CI gate)
hyp context       # regenerate .hyp/context.yaml for AI tools
` + "```" + ``

const goModContent = `module {{.ProjectName}}

go 1.24

require github.com/maoxiaoyue/hypgo v0.8.11
`

const createUsersUpSQL = `
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    avatar VARCHAR(500),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);

-- 更新時間觸發器
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_users_updated_at BEFORE UPDATE
    ON users FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`

const createUsersDownSQL = `DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS users;`

const createRolesUpSQL = `CREATE TABLE IF NOT EXISTS roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS permissions (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    resource VARCHAR(100),
    action VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    role_id INTEGER REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id INTEGER REFERENCES roles(id) ON DELETE CASCADE,
    permission_id INTEGER REFERENCES permissions(id) ON DELETE CASCADE,
    granted_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, permission_id)
);

-- 插入默認角色
INSERT INTO roles (name, description) VALUES 
    ('admin', 'Administrator with full access'),
    ('user', 'Regular user with limited access'),
    ('moderator', 'Moderator with content management access')
ON CONFLICT (name) DO NOTHING;

-- 插入默認權限
INSERT INTO permissions (name, resource, action) VALUES 
    ('users.read', 'users', 'read'),
    ('users.write', 'users', 'write'),
    ('users.delete', 'users', 'delete'),
    ('posts.read', 'posts', 'read'),
    ('posts.write', 'posts', 'write'),
    ('posts.delete', 'posts', 'delete')
ON CONFLICT (name) DO NOTHING;

-- 為 admin 角色分配所有權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

-- 為 user 角色分配讀取權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'user' AND p.action = 'read'
ON CONFLICT DO NOTHING;`

const createRolesDownSQL = `DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;`
