package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"text/template"
)

var apiCmd = &cobra.Command{
	Use:   "api [project-name]",
	Short: "Create a new HypGo API-only project",
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
	configContent := `# HypGo API Configuration

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
  output: logs/api.log  # stdout or file path
  colors: true
  rotation:
    max_size: 100MB
    max_age: 7d
    max_backups: 10
    compress: true

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

# 插件配置將存放在獨立的文件中
# 使用 'hyp addp <plugin-name>' 來添加插件
# 支援的插件：rabbitmq, kafka, cassandra, scylladb, mongodb, elasticsearch
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

    "github.com/maoxiaoyue/hypgo/pkg/config"
    "github.com/maoxiaoyue/hypgo/pkg/database"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
    "github.com/maoxiaoyue/hypgo/pkg/server"
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
    router.Use(middleware.Logger(log))
    router.Use(middleware.CORS())
    router.Use(middleware.RateLimit())
    
    // 註冊 API 路由
    controllers.RegisterAPIRoutes(router, db, log)

    // 啟動服務器
    go func() {
        log.Info("Starting HypGo API server...")
        if err := srv.Start(); err != nil {
            log.Emergency("Server error: %v", err)
            os.Exit(1)
        }
    }()

    // 優雅關閉
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Info("Shutting down server...")
    
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := srv.Shutdown(ctx); err != nil {
        log.Emergency("Server forced to shutdown: %v", err)
    }

    log.Info("Server exited")
}
`

	return createTemplateFile(projectName, "main.go", mainContent, map[string]string{
		"ProjectName": projectName,
	})
}

func createAPIController(projectName string) error {
	controllerContent := `package controllers

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/gorilla/mux"
    "github.com/maoxiaoyue/hypgo/pkg/database"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
    "github.com/maoxiaoyue/hypgo/pkg/websocket"
)

type APIResponse struct {
    Success bool        ` + "`json:\"success\"`" + `
    Message string      ` + "`json:\"message\"`" + `
    Data    interface{} ` + "`json:\"data,omitempty\"`" + `
    Error   string      ` + "`json:\"error,omitempty\"`" + `
}

type APIController struct {
    db     *database.Database
    logger *logger.Logger
    wsHub  *websocket.Hub
}

func RegisterAPIRoutes(router *mux.Router, db *database.Database, log *logger.Logger) {
    // 初始化 WebSocket Hub
    wsHub := websocket.NewHub(log)
    go wsHub.Run()

    controller := &APIController{
        db:     db,
        logger: log,
        wsHub:  wsHub,
    }

    // API 版本前綴
    api := router.PathPrefix("/api/v1").Subrouter()
    
    // Health check
    api.HandleFunc("/health", controller.HealthCheck).Methods("GET")
    
    // REST API endpoints
    api.HandleFunc("/users", controller.GetUsers).Methods("GET")
    api.HandleFunc("/users", controller.CreateUser).Methods("POST")
    api.HandleFunc("/users/{id}", controller.GetUser).Methods("GET")
    api.HandleFunc("/users/{id}", controller.UpdateUser).Methods("PUT")
    api.HandleFunc("/users/{id}", controller.DeleteUser).Methods("DELETE")
    
    // WebSocket endpoint
    api.HandleFunc("/ws", websocket.AuthMiddleware(wsHub.ServeWS)).Methods("GET")
    
    // 實時通知端點
    api.HandleFunc("/notify", controller.SendNotification).Methods("POST")
}

func (c *APIController) HealthCheck(w http.ResponseWriter, r *http.Request) {
    c.sendJSON(w, http.StatusOK, APIResponse{
        Success: true,
        Message: "API is healthy",
        Data: map[string]interface{}{
            "timestamp": time.Now().Unix(),
            "version":   "1.0.0",
            "protocol":  r.Proto,
        },
    })
}

func (c *APIController) GetUsers(w http.ResponseWriter, r *http.Request) {
    // TODO: 實現從數據庫獲取用戶列表
    users := []map[string]interface{}{
        {"id": 1, "name": "User 1", "email": "user1@example.com"},
        {"id": 2, "name": "User 2", "email": "user2@example.com"},
    }
    
    c.sendJSON(w, http.StatusOK, APIResponse{
        Success: true,
        Message: "Users retrieved successfully",
        Data:    users,
    })
}

func (c *APIController) CreateUser(w http.ResponseWriter, r *http.Request) {
    var user map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
        c.sendError(w, http.StatusBadRequest, "Invalid request body")
        return
    }
    
    // TODO: 實現用戶創建邏輯
    
    c.sendJSON(w, http.StatusCreated, APIResponse{
        Success: true,
        Message: "User created successfully",
        Data:    user,
    })
}

func (c *APIController) GetUser(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    // TODO: 實現從數據庫獲取單個用戶
    
    c.sendJSON(w, http.StatusOK, APIResponse{
        Success: true,
        Message: "User retrieved successfully",
        Data: map[string]interface{}{
            "id":    id,
            "name":  "User " + id,
            "email": "user" + id + "@example.com",
        },
    })
}

func (c *APIController) UpdateUser(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    var updates map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
        c.sendError(w, http.StatusBadRequest, "Invalid request body")
        return
    }
    
    // TODO: 實現用戶更新邏輯
    
    c.sendJSON(w, http.StatusOK, APIResponse{
        Success: true,
        Message: "User updated successfully",
        Data: map[string]interface{}{
            "id":      id,
            "updated": updates,
        },
    })
}

func (c *APIController) DeleteUser(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    id := vars["id"]
    
    // TODO: 實現用戶刪除邏輯
    
    c.sendJSON(w, http.StatusNoContent, nil)
}

func (c *APIController) SendNotification(w http.ResponseWriter, r *http.Request) {
    var notification struct {
        Channel string          ` + "`json:\"channel\"`" + `
        Message json.RawMessage ` + "`json:\"message\"`" + `
    }
    
    if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
        c.sendError(w, http.StatusBadRequest, "Invalid request body")
        return
    }
    
    // 通過 WebSocket 發送通知
    if err := c.wsHub.PublishToChannelJSON(notification.Channel, notification.Message); err != nil {
        c.sendError(w, http.StatusInternalServerError, "Failed to send notification")
        return
    }
    
    c.sendJSON(w, http.StatusOK, APIResponse{
        Success: true,
        Message: "Notification sent successfully",
    })
}

func (c *APIController) sendJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    
    if data != nil {
        json.NewEncoder(w).Encode(data)
    }
}

func (c *APIController) sendError(w http.ResponseWriter, status int, message string) {
    c.sendJSON(w, status, APIResponse{
        Success: false,
        Error:   message,
    })
}
`

	filename := filepath.Join(projectName, "app", "controllers", "api.go")
	return os.WriteFile(filename, []byte(controllerContent), 0644)
}

func createMiddleware(projectName string) error {
	middlewareContent := `package middleware

import (
    "net/http"
    "time"
    "sync"
    "strings"

    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

// Logger 中間件
func Logger(log *logger.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            
            // 包裝 ResponseWriter 以記錄狀態碼
            wrapped := &responseWriter{
                ResponseWriter: w,
                statusCode:    http.StatusOK,
            }
            
            next.ServeHTTP(wrapped, r)
            
            duration := time.Since(start)
            log.Info("%s %s %d %v", r.Method, r.URL.Path, wrapped.statusCode, duration)
        })
    }
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.statusCode = code
    rw.ResponseWriter.WriteHeader(code)
}

// CORS 中間件
func CORS() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Access-Control-Allow-Origin", "*")
            w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
            w.Header().Set("Access-Control-Max-Age", "86400")
            
            if r.Method == http.MethodOptions {
                w.WriteHeader(http.StatusNoContent)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}

// RateLimit 中間件
var (
    visitors = make(map[string]*visitor)
    mu       sync.Mutex
)

type visitor struct {
    lastSeen time.Time
    count    int
}

func RateLimit() func(http.Handler) http.Handler {
    // 清理過期的訪問者
    go func() {
        for {
            time.Sleep(time.Minute)
            mu.Lock()
            for ip, v := range visitors {
                if time.Since(v.lastSeen) > time.Minute {
                    delete(visitors, ip)
                }
            }
            mu.Unlock()
        }
    }()
    
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := r.RemoteAddr
            
            mu.Lock()
            v, exists := visitors[ip]
            if !exists {
                visitors[ip] = &visitor{lastSeen: time.Now(), count: 1}
                mu.Unlock()
                next.ServeHTTP(w, r)
                return
            }
            
            if time.Since(v.lastSeen) > time.Minute {
                v.count = 1
                v.lastSeen = time.Now()
            } else {
                v.count++
                if v.count > 60 { // 每分鐘 60 個請求
                    mu.Unlock()
                    http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
                    return
                }
            }
            v.lastSeen = time.Now()
            mu.Unlock()
            
            next.ServeHTTP(w, r)
        })
    }
}

// Auth 中間件（JWT 驗證）
func Auth() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization")
            
            if token == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // 移除 "Bearer " 前綴
            if strings.HasPrefix(token, "Bearer ") {
                token = strings.TrimPrefix(token, "Bearer ")
            }
            
            // TODO: 實現 JWT 驗證邏輯
            // 1. 解析 token
            // 2. 驗證簽名
            // 3. 檢查過期時間
            // 4. 將用戶信息添加到 context
            
            next.ServeHTTP(w, r)
        })
    }
}
`

	filename := filepath.Join(projectName, "app", "middleware", "middleware.go")
	return os.WriteFile(filename, []byte(middlewareContent), 0644)
}

func createTemplateFile(projectName, filename, content string, data interface{}) error {
	tmpl, err := template.New("file").Parse(content)
	if err != nil {
		return err
	}

	file, err := os.Create(filepath.Join(projectName, filename))
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}
