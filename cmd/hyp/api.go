package main

import (
	"fmt"
	//	"golang.org/x/crypto/bcrypt"
	"os"
	"path/filepath"
	"text/template"
	//	"time"

	"github.com/spf13/cobra"
)

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
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "app", "middleware"),
		filepath.Join(projectName, "app", "validators"),
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, "internal", "database"),
		filepath.Join(projectName, "internal", "logger"),
		filepath.Join(projectName, "internal", "cache"),
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

	// 創建所有必要的檔案
	files := []fileTemplate{
		// 主要檔案
		{Path: "main.go", Content: mainGoContent},
		{Path: "config/config.yaml", Content: configYamlContent},
		{Path: ".env.example", Content: envExampleContent},

		// 初始化檔案
		{Path: "internal/logger/init.go", Content: loggerInitContent},
		{Path: "app/models/init.go", Content: modelsInitContent},
		{Path: "internal/database/init.go", Content: databaseInitContent},
		{Path: "internal/cache/init.go", Content: cacheInitContent},

		// 控制器和中間件
		{Path: "app/controllers/api.go", Content: apiControllerContent},
		{Path: "app/controllers/health.go", Content: healthControllerContent},
		{Path: "app/middleware/middleware.go", Content: middlewareContent},
		{Path: "app/middleware/auth.go", Content: authMiddlewareContent},

		// 模型和服務
		{Path: "app/models/user.go", Content: userModelContent},
		{Path: "app/services/user_service.go", Content: userServiceContent},
		{Path: "app/services/auth_service.go", Content: authServiceContent},
		{Path: "app/validators/user_validator.go", Content: userValidatorContent},

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

	// 打印成功信息
	printSuccessMessage(projectName)

	return nil
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
	fmt.Printf("   │   ├── controllers/    # API controllers with new Context\n")
	fmt.Printf("   │   ├── models/         # Data models with DB init\n")
	fmt.Printf("   │   ├── services/       # Business logic layer\n")
	fmt.Printf("   │   ├── middleware/     # HTTP middleware\n")
	fmt.Printf("   │   └── validators/     # Request validators\n")
	fmt.Printf("   ├── internal/\n")
	fmt.Printf("   │   ├── logger/         # Logger initialization\n")
	fmt.Printf("   │   ├── database/       # Database connections\n")
	fmt.Printf("   │   └── cache/          # Redis cache\n")
	fmt.Printf("   ├── config/             # Configuration files\n")
	fmt.Printf("   ├── migrations/         # Database migrations\n")
	fmt.Printf("   ├── tests/              # Test files\n")
	fmt.Printf("   ├── docs/               # API documentation\n")
	fmt.Printf("   ├── logs/               # Log files\n")
	fmt.Printf("   └── main.go             # Entry point\n")
	fmt.Printf("\n🚀 Quick Start:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   cp .env.example .env    # Configure environment\n")
	fmt.Printf("   make install-tools      # Install dev tools\n")
	fmt.Printf("   make cert              # Generate certificates\n")
	fmt.Printf("   make migrate           # Run migrations\n")
	fmt.Printf("   make dev               # Start with hot reload\n")
	fmt.Printf("\n📦 Available Commands:\n")
	fmt.Printf("   make build             # Build binary\n")
	fmt.Printf("   make test              # Run tests\n")
	fmt.Printf("   make docker            # Build Docker image\n")
	fmt.Printf("   make docker-compose-up # Start all services\n")
	fmt.Printf("\n🌟 Features:\n")
	fmt.Printf("   • HTTP/3 with QUIC support\n")
	fmt.Printf("   • New Context architecture\n")
	fmt.Printf("   • Auto-rotating logs with size limits\n")
	fmt.Printf("   • PostgreSQL + Redis integration\n")
	fmt.Printf("   • JWT authentication\n")
	fmt.Printf("   • Rate limiting & CORS\n")
	fmt.Printf("   • WebSocket support\n")
	fmt.Printf("   • Graceful shutdown\n")
	fmt.Printf("   • Docker ready\n")
	fmt.Printf("\n")
}

// ===== File Contents =====

const mainGoContent = `package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"{{.ProjectName}}/app/controllers"
	"{{.ProjectName}}/app/middleware"
	"{{.ProjectName}}/app/models"
	"{{.ProjectName}}/config"
	"{{.ProjectName}}/internal/cache"
	"{{.ProjectName}}/internal/database"
	"{{.ProjectName}}/internal/logger"
	
	"github.com/maoxiaoyue/hypgo/pkg/server"
	hypContext "github.com/maoxiaoyue/hypgo/context"
)

func main() {
	// 載入配置
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日誌系統
	log, err := logger.Init(cfg.Logger)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	log.Info("Starting HypGo API Server...")

	// 設置 Context 運行模式
	switch cfg.Logger.Level {
	case "debug":
		hypContext.SetMode("debug")
	case "info", "notice":
		hypContext.SetMode("test")
	default:
		hypContext.SetMode("release")
	}

	// 初始化數據庫
	db, err := database.Init(cfg.Database)
	if err != nil {
		log.Emergency("Failed to initialize database: %v", err)
		os.Exit(1)
	}
	defer database.Close()

	// 初始化 Redis
	if err := cache.Init(cfg.Redis); err != nil {
		log.Warning("Failed to initialize Redis: %v", err)
		// Redis 是可選的，不退出
	}
	defer cache.Close()

	// 自動遷移數據庫
	if cfg.Database.AutoMigrate {
		log.Info("Running database migrations...")
		if err := models.AutoMigrate(db); err != nil {
			log.Error("Failed to migrate database: %v", err)
		}
	}

	// 創建服務器
	srv := server.NewWithContext(cfg, log)
	
	// 設置路由
	setupRoutes(srv, cfg, log)

	// 啟動服務器
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("Server starting on %s with protocol %s", 
			cfg.Server.Addr, 
			cfg.Server.Protocol)
		
		switch cfg.Server.Protocol {
		case "http3":
			if cfg.Server.TLS.Enabled {
				serverErrors <- srv.StartHTTP3(
					cfg.Server.Addr,
					cfg.Server.TLS.CertFile,
					cfg.Server.TLS.KeyFile,
				)
			} else {
				log.Warning("HTTP/3 requires TLS, falling back to HTTP/2")
				serverErrors <- srv.StartHTTP2(cfg.Server.Addr)
			}
		case "http2":
			if cfg.Server.TLS.Enabled {
				serverErrors <- srv.StartTLS(
					cfg.Server.Addr,
					cfg.Server.TLS.CertFile,
					cfg.Server.TLS.KeyFile,
				)
			} else {
				serverErrors <- srv.Start(cfg.Server.Addr)
			}
		default:
			serverErrors <- srv.Start(cfg.Server.Addr)
		}
	}()

	// 優雅關閉
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Emergency("Server error: %v", err)
		os.Exit(1)
	case sig := <-shutdown:
		log.Info("Shutdown signal received: %v", sig)
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("Server forced to shutdown: %v", err)
			os.Exit(1)
		}
		
		log.Info("Server stopped gracefully")
	}
}

func setupRoutes(srv *server.Server, cfg *config.Config, log logger.Logger) {
	router := srv.Router()
	
	// 全局中間件
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger(log))
	router.Use(middleware.Recovery())
	router.Use(middleware.CORS(cfg.API.CORS))
	router.Use(middleware.Security())
	router.Use(middleware.Metrics())
	
	// 健康檢查（不需要認證）
	router.GET("/health", controllers.HealthCheck)
	router.GET("/metrics", controllers.Metrics)
	
	// API 路由組
	api := router.Group("/api/v1")
	
	// 公開路由
	auth := api.Group("/auth")
	{
		auth.POST("/register", controllers.Register)
		auth.POST("/login", controllers.Login)
		auth.POST("/refresh", controllers.RefreshToken)
	}
	
	// 需要認證的路由
	protected := api.Group("")
	protected.Use(middleware.Auth(cfg.API.JWT.Secret))
	{
		// 用戶管理
		protected.GET("/users", controllers.GetUsers)
		protected.POST("/users", middleware.RequireRole("admin"), controllers.CreateUser)
		protected.GET("/users/:id", controllers.GetUser)
		protected.PUT("/users/:id", controllers.UpdateUser)
		protected.DELETE("/users/:id", middleware.RequireRole("admin"), controllers.DeleteUser)
		
		// 登出
		protected.POST("/auth/logout", controllers.Logout)
		
		// WebSocket
		protected.GET("/ws", controllers.WebSocket)
	}
	
	// 限流路由
	if cfg.API.RateLimit.Enabled {
		api.Use(middleware.RateLimit(cfg.API.RateLimit))
	}
}
`

const loggerInitContent = `package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Notice(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Error(format string, args ...interface{})
	Emergency(format string, args ...interface{})
	Close() error
}

type Config struct {
	Level       string ` + "`yaml:\"level\" json:\"level\"`" + `
	Output      string ` + "`yaml:\"output\" json:\"output\"`" + `
	File        string ` + "`yaml:\"file\" json:\"file\"`" + `
	MaxSize     int    ` + "`yaml:\"max_size\" json:\"max_size\"`" + `         // megabytes
	MaxAge      int    ` + "`yaml:\"max_age\" json:\"max_age\"`" + `           // days
	MaxBackups  int    ` + "`yaml:\"max_backups\" json:\"max_backups\"`" + `
	Compress    bool   ` + "`yaml:\"compress\" json:\"compress\"`" + `
	Format      string ` + "`yaml:\"format\" json:\"format\"`" + `             // json or text
	Colors      bool   ` + "`yaml:\"colors\" json:\"colors\"`" + `
	TimeFormat  string ` + "`yaml:\"time_format\" json:\"time_format\"`" + `
}

var (
	instance Logger
)

// Init 初始化日誌系統
func Init(cfg Config) (Logger, error) {
	// 設置默認值
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Output == "" {
		cfg.Output = "stdout"
	}
	if cfg.TimeFormat == "" {
		cfg.TimeFormat = "2006-01-02 15:04:05"
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 100 // 100MB
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 30 // 30 days
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 10
	}

	// 創建輸出 writer
	var writer io.Writer
	
	switch cfg.Output {
	case "file":
		writer = createFileWriter(cfg)
	case "both":
		writer = io.MultiWriter(os.Stdout, createFileWriter(cfg))
	default:
		writer = os.Stdout
	}

	// 創建 logger 實例
	log, err := logger.New(
		cfg.Level,
		writer,
		&logger.RotationConfig{
			MaxSize:    cfg.MaxSize,
			MaxAge:     cfg.MaxAge,
			MaxBackups: cfg.MaxBackups,
			Compress:   cfg.Compress,
		},
		cfg.Colors,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	instance = log
	return log, nil
}

// createFileWriter 創建文件 writer
func createFileWriter(cfg Config) io.Writer {
	// 確保日誌目錄存在
	dir := filepath.Dir(cfg.File)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Failed to create log directory: %v\n", err)
	}

	// 生成帶日期的檔名
	filename := generateLogFilename(cfg.File)

	// 使用 lumberjack 進行日誌輪轉
	return &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    cfg.MaxSize,    // megabytes
		MaxAge:     cfg.MaxAge,      // days
		MaxBackups: cfg.MaxBackups,
		LocalTime:  true,
		Compress:   cfg.Compress,
	}
}

// generateLogFilename 生成帶日期的日誌檔名
func generateLogFilename(baseFile string) string {
	dir := filepath.Dir(baseFile)
	ext := filepath.Ext(baseFile)
	name := filepath.Base(baseFile)
	
	if ext != "" {
		name = name[:len(name)-len(ext)]
	}
	
	// 添加日期到檔名
	dateStr := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s-%s%s", name, dateStr, ext)
	
	return filepath.Join(dir, filename)
}

// Get 獲取 logger 實例
func Get() Logger {
	if instance == nil {
		// 如果未初始化，返回默認 logger
		log, _ := Init(Config{})
		return log
	}
	return instance
}

// 便利函數
func Debug(format string, args ...interface{}) {
	Get().Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	Get().Info(format, args...)
}

func Warning(format string, args ...interface{}) {
	Get().Warning(format, args...)
}

func Error(format string, args ...interface{}) {
	Get().Error(format, args...)
}
`

const modelsInitContent = `package models

import (
	"fmt"
	
	"gorm.io/gorm"
	"{{.ProjectName}}/internal/database"
)

// AutoMigrate 自動遷移所有模型
func AutoMigrate(db *gorm.DB) error {
	models := []interface{}{
		&User{},
		&Role{},
		&Permission{},
	}
	
	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("failed to migrate %T: %w", model, err)
		}
	}
	
	// 創建默認角色
	if err := createDefaultRoles(db); err != nil {
		return fmt.Errorf("failed to create default roles: %w", err)
	}
	
	return nil
}

// createDefaultRoles 創建默認角色
func createDefaultRoles(db *gorm.DB) error {
	defaultRoles := []Role{
		{Name: "admin", Description: "Administrator with full access"},
		{Name: "user", Description: "Regular user with limited access"},
	}
	
	for _, role := range defaultRoles {
		var count int64
		db.Model(&Role{}).Where("name = ?", role.Name).Count(&count)
		if count == 0 {
			if err := db.Create(&role).Error; err != nil {
				return err
			}
		}
	}
	
	return nil
}

// GetDB 獲取數據庫實例
func GetDB() *gorm.DB {
	return database.GetDB()
}
`

const databaseInitContent = `package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	Driver          string ` + "`yaml:\"driver\" json:\"driver\"`" + `
	DSN             string ` + "`yaml:\"dsn\" json:\"dsn\"`" + `
	MaxIdleConns    int    ` + "`yaml:\"max_idle_conns\" json:\"max_idle_conns\"`" + `
	MaxOpenConns    int    ` + "`yaml:\"max_open_conns\" json:\"max_open_conns\"`" + `
	ConnMaxLifetime string ` + "`yaml:\"conn_max_lifetime\" json:\"conn_max_lifetime\"`" + `
	LogLevel        string ` + "`yaml:\"log_level\" json:\"log_level\"`" + `
	AutoMigrate     bool   ` + "`yaml:\"auto_migrate\" json:\"auto_migrate\"`" + `
}

var (
	db *gorm.DB
)

// Init 初始化數據庫連接
func Init(cfg Config) (*gorm.DB, error) {
	// 設置默認值
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 100
	}
	if cfg.ConnMaxLifetime == "" {
		cfg.ConnMaxLifetime = "1h"
	}

	// 解析連接生命週期
	lifetime, err := time.ParseDuration(cfg.ConnMaxLifetime)
	if err != nil {
		lifetime = time.Hour
	}

	// 設置日誌級別
	logLevel := logger.Silent
	switch cfg.LogLevel {
	case "debug":
		logLevel = logger.Info
	case "info":
		logLevel = logger.Warn
	case "warn", "warning":
		logLevel = logger.Error
	}

	// GORM 配置
	gormConfig := &gorm.Config{
		Logger:                 logger.Default.LogMode(logLevel),
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	}

	// 根據驅動創建連接
	switch cfg.Driver {
	case "postgres", "postgresql":
		db, err = gorm.Open(postgres.Open(cfg.DSN), gormConfig)
	case "mysql":
		db, err = gorm.Open(mysql.Open(cfg.DSN), gormConfig)
	case "sqlite", "sqlite3":
		db, err = gorm.Open(sqlite.Open(cfg.DSN), gormConfig)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 獲取底層 SQL 數據庫
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying SQL database: %w", err)
	}

	// 設置連接池
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(lifetime)

	// 測試連接
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// GetDB 獲取數據庫實例
func GetDB() *gorm.DB {
	return db
}

// Close 關閉數據庫連接
func Close() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Transaction 執行事務
func Transaction(fn func(*gorm.DB) error) error {
	return db.Transaction(fn)
}
`

const cacheInitContent = `package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr     string ` + "`yaml:\"addr\" json:\"addr\"`" + `
	Password string ` + "`yaml:\"password\" json:\"password\"`" + `
	DB       int    ` + "`yaml:\"db\" json:\"db\"`" + `
	PoolSize int    ` + "`yaml:\"pool_size\" json:\"pool_size\"`" + `
	MinIdleConns int ` + "`yaml:\"min_idle_conns\" json:\"min_idle_conns\"`" + `
	MaxRetries int ` + "`yaml:\"max_retries\" json:\"max_retries\"`" + `
	DialTimeout  string ` + "`yaml:\"dial_timeout\" json:\"dial_timeout\"`" + `
	ReadTimeout  string ` + "`yaml:\"read_timeout\" json:\"read_timeout\"`" + `
	WriteTimeout string ` + "`yaml:\"write_timeout\" json:\"write_timeout\"`" + `
}

var (
	client *redis.Client
	ctx    = context.Background()
)

// Init 初始化 Redis 連接
func Init(cfg Config) error {
	// 設置默認值
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}
	if cfg.PoolSize == 0 {
		cfg.PoolSize = 10
	}
	if cfg.MinIdleConns == 0 {
		cfg.MinIdleConns = 5
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}

	// 解析超時時間
	dialTimeout, _ := time.ParseDuration(cfg.DialTimeout)
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}
	
	readTimeout, _ := time.ParseDuration(cfg.ReadTimeout)
	if readTimeout == 0 {
		readTimeout = 3 * time.Second
	}
	
	writeTimeout, _ := time.ParseDuration(cfg.WriteTimeout)
	if writeTimeout == 0 {
		writeTimeout = 3 * time.Second
	}

	// 創建 Redis 客戶端
	client = redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	})

	// 測試連接
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// GetClient 獲取 Redis 客戶端
func GetClient() *redis.Client {
	return client
}

// Close 關閉 Redis 連接
func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// Set 設置鍵值
func Set(key string, value interface{}, expiration time.Duration) error {
	return client.Set(ctx, key, value, expiration).Err()
}

// Get 獲取值
func Get(key string) (string, error) {
	return client.Get(ctx, key).Result()
}

// Delete 刪除鍵
func Delete(keys ...string) error {
	return client.Del(ctx, keys...).Err()
}

// Exists 檢查鍵是否存在
func Exists(keys ...string) (int64, error) {
	return client.Exists(ctx, keys...).Result()
}

// SetNX 只在鍵不存在時設置
func SetNX(key string, value interface{}, expiration time.Duration) (bool, error) {
	return client.SetNX(ctx, key, value, expiration).Result()
}

// Incr 增加計數
func Incr(key string) (int64, error) {
	return client.Incr(ctx, key).Result()
}

// Expire 設置過期時間
func Expire(key string, expiration time.Duration) error {
	return client.Expire(ctx, key, expiration).Err()
}
`

// 其他檔案內容常量...

const configYamlContent = `# HypGo API Configuration

server:
  protocol: http3         # http1, http2, http3
  addr: :8080
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  keep_alive: 30s
  max_handlers: 1000
  max_concurrent_streams: 100
  max_read_frame_size: 1048576
  enable_graceful_restart: true
  tls:
    enabled: true
    cert_file: "certs/server.crt"
    key_file: "certs/server.key"
    auto_cert: false
    domains: []

database:
  driver: postgres        # postgres, mysql, sqlite
  dsn: "${DB_DSN}"
  max_idle_conns: 10
  max_open_conns: 100
  conn_max_lifetime: 1h
  log_level: warning      # silent, error, warning, info
  auto_migrate: true

redis:
  addr: "${REDIS_ADDR}"
  password: "${REDIS_PASSWORD}"
  db: 0
  pool_size: 10
  min_idle_conns: 5
  max_retries: 3
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s

logger:
  level: debug            # debug, info, notice, warning, error, emergency
  output: both            # stdout, file, both
  file: logs/api.log
  max_size: 100           # MB
  max_age: 30             # days
  max_backups: 10
  compress: true
  format: json            # json, text
  colors: true
  time_format: "2006-01-02 15:04:05"

api:
  version: "v1"
  docs_enabled: true
  docs_path: "/docs"
  
  rate_limit:
    enabled: true
    requests_per_minute: 60
    burst: 10
    
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
      - PATCH
    allowed_headers:
      - Content-Type
      - Authorization
      - X-Request-ID
    expose_headers:
      - X-Request-ID
      - X-RateLimit-Limit
      - X-RateLimit-Remaining
    max_age: 86400
    
  jwt:
    secret: "${JWT_SECRET}"
    issuer: "hypgo-api"
    expiration: 24h
    refresh_expiration: 720h

monitoring:
  metrics_enabled: true
  metrics_path: "/metrics"
  health_path: "/health"
  trace_enabled: false
  trace_provider: "jaeger"
  trace_endpoint: "${TRACE_ENDPOINT}"
`

const envExampleContent = `# Server Configuration
ENV=development
SERVER_ADDR=:8080
SERVER_PROTOCOL=http3

# Database Configuration
DB_DRIVER=postgres
DB_DSN=postgres://hypgo:password@localhost:5432/hypgo_db?sslmode=disable

# Redis Configuration
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# JWT Configuration
JWT_SECRET=change-this-to-a-secure-secret-key-at-least-32-characters
JWT_ISSUER=hypgo-api
JWT_EXPIRATION=24h

# CORS Configuration
CORS_ALLOWED_ORIGINS=*
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS,PATCH
CORS_ALLOWED_HEADERS=Content-Type,Authorization,X-Request-ID

# Rate Limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS_PER_MINUTE=60

# TLS Configuration
TLS_ENABLED=true
TLS_CERT_FILE=certs/server.crt
TLS_KEY_FILE=certs/server.key
TLS_AUTO_CERT=false

# Monitoring
METRICS_ENABLED=true
TRACE_ENABLED=false
TRACE_ENDPOINT=http://localhost:14268/api/traces

# Logging
LOG_LEVEL=debug
LOG_OUTPUT=both
LOG_FILE=logs/api.log
`

const apiControllerContent = `package controllers

import (
	"net/http"
	"strconv"
	
	"github.com/maoxiaoyue/hypgo/context"
	"{{.ProjectName}}/app/models"
	"{{.ProjectName}}/app/services"
	"{{.ProjectName}}/internal/database"
	"{{.ProjectName}}/internal/logger"
)

// GetUsers 獲取用戶列表
func GetUsers(ctx *context.Context) {
	// 獲取分頁參數
	page := ctx.GetPage()
	pageSize := ctx.GetPageSize()
	
	// 從服務層獲取數據
	userService := services.NewUserService(database.GetDB())
	users, total, err := userService.GetUsers(page, pageSize)
	if err != nil {
		logger.Error("Failed to get users: %v", err)
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, context.H{
			"error": "Failed to retrieve users",
		})
		return
	}
	
	// 設置響應頭
	ctx.Header("X-Total-Count", strconv.Itoa(total))
	
	ctx.JSON(http.StatusOK, context.H{
		"success": true,
		"data":    users,
		"meta": context.H{
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// GetUser 獲取單個用戶
func GetUser(ctx *context.Context) {
	userID, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "Invalid user ID",
		})
		return
	}
	
	userService := services.NewUserService(database.GetDB())
	user, err := userService.GetUserByID(userID)
	if err != nil {
		if err == services.ErrNotFound {
			ctx.AbortWithStatusJSON(http.StatusNotFound, context.H{
				"error": "User not found",
			})
		} else {
			logger.Error("Failed to get user: %v", err)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, context.H{
				"error": "Failed to retrieve user",
			})
		}
		return
	}
	
	ctx.JSON(http.StatusOK, context.H{
		"success": true,
		"data":    user,
	})
}

// CreateUser 創建用戶
func CreateUser(ctx *context.Context) {
	var req models.CreateUserRequest
	
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "Invalid request data",
			"details": err.Error(),
		})
		return
	}
	
	userService := services.NewUserService(database.GetDB())
	user, err := userService.CreateUser(req)
	if err != nil {
		if err == services.ErrDuplicate {
			ctx.AbortWithStatusJSON(http.StatusConflict, context.H{
				"error": "User already exists",
			})
		} else {
			logger.Error("Failed to create user: %v", err)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, context.H{
				"error": "Failed to create user",
			})
		}
		return
	}
	
	ctx.JSON(http.StatusCreated, context.H{
		"success": true,
		"message": "User created successfully",
		"data":    user,
	})
}

// UpdateUser 更新用戶
func UpdateUser(ctx *context.Context) {
	userID, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "Invalid user ID",
		})
		return
	}
	
	// 檢查權限
	currentUserID := ctx.GetInt("user_id")
	if currentUserID != userID && !ctx.HasRole("admin") {
		ctx.AbortWithStatusJSON(http.StatusForbidden, context.H{
			"error": "Permission denied",
		})
		return
	}
	
	var req models.UpdateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "Invalid request data",
		})
		return
	}
	
	userService := services.NewUserService(database.GetDB())
	user, err := userService.UpdateUser(userID, req)
	if err != nil {
		if err == services.ErrNotFound {
			ctx.AbortWithStatusJSON(http.StatusNotFound, context.H{
				"error": "User not found",
			})
		} else {
			logger.Error("Failed to update user: %v", err)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, context.H{
				"error": "Failed to update user",
			})
		}
		return
	}
	
	ctx.JSON(http.StatusOK, context.H{
		"success": true,
		"message": "User updated successfully",
		"data":    user,
	})
}

// DeleteUser 刪除用戶
func DeleteUser(ctx *context.Context) {
	userID, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "Invalid user ID",
		})
		return
	}
	
	userService := services.NewUserService(database.GetDB())
	if err := userService.DeleteUser(userID); err != nil {
		if err == services.ErrNotFound {
			ctx.AbortWithStatusJSON(http.StatusNotFound, context.H{
				"error": "User not found",
			})
		} else {
			logger.Error("Failed to delete user: %v", err)
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, context.H{
				"error": "Failed to delete user",
			})
		}
		return
	}
	
	ctx.Status(http.StatusNoContent)
}

// WebSocket WebSocket 連接處理
func WebSocket(ctx *context.Context) {
	if !ctx.IsWebsocket() {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, context.H{
			"error": "WebSocket connection required",
		})
		return
	}
	
	// TODO: 實現 WebSocket 邏輯
	ctx.JSON(http.StatusNotImplemented, context.H{
		"error": "WebSocket not implemented yet",
	})
}
`
const middlewareContent = `package middleware`
const healthControllerContent = `package controllers

import (
	"net/http"
	"runtime"
	"time"
	
	"github.com/maoxiaoyue/hypgo/context"
	"{{.ProjectName}}/internal/cache"
	"{{.ProjectName}}/internal/database"
)

// HealthCheck 健康檢查
func HealthCheck(ctx *context.Context) {
	// 檢查數據庫
	dbStatus := "healthy"
	if db := database.GetDB(); db != nil {
		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil {
			dbStatus = "unhealthy"
		}
	} else {
		dbStatus = "not connected"
	}
	
	// 檢查 Redis
	redisStatus := "healthy"
	if client := cache.GetClient(); client != nil {
		if err := client.Ping(client.Context()).Err(); err != nil {
			redisStatus = "unhealthy"
		}
	} else {
		redisStatus = "not connected"
	}
	
	// 系統信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	ctx.JSON(http.StatusOK, context.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"protocol":  ctx.Protocol(),
		"services": context.H{
			"database": dbStatus,
			"redis":    redisStatus,
		},
		"system": context.H{
			"goroutines":   runtime.NumGoroutine(),
			"memory_alloc": m.Alloc / 1024 / 1024,      // MB
			"memory_sys":   m.Sys / 1024 / 1024,        // MB
			"gc_runs":      m.NumGC,
		},
		"features": context.H{
			"http3":     ctx.IsHTTP3(),
			"http2":     ctx.IsHTTP2(),
			"websocket": false,
		},
	})
}

// Metrics Prometheus 指標
func Metrics(ctx *context.Context) {
	// TODO: 實現 Prometheus 指標
	ctx.String(http.StatusOK, "# HELP api_requests_total Total number of API requests\n")
}
`

const authMiddlewareContent = `package middleware

import (
	"net/http"
	"strings"
	
	"github.com/golang-jwt/jwt/v5"
	"github.com/maoxiaoyue/hypgo/context"
)

// Auth JWT 認證中間件
func Auth(secret string) context.HandlerFunc {
	return func(ctx *context.Context) {
		token := ctx.GetJWT()
		
		if token == "" {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, context.H{
				"error": "Authorization required",
			})
			return
		}
		
		// 解析 JWT
		claims, err := parseJWT(token, secret)
		if err != nil {
			ctx.SetAuthError(err.Error())
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, context.H{
				"error": "Invalid or expired token",
			})
			return
		}
		
		// 設置用戶信息
		if userID, ok := claims["user_id"].(float64); ok {
			ctx.SetUserID(int(userID))
		}
		
		if username, ok := claims["username"].(string); ok {
			ctx.SetUser(username)
		}
		
		if roles, ok := claims["roles"].([]interface{}); ok {
			stringRoles := make([]string, len(roles))
			for i, role := range roles {
				if s, ok := role.(string); ok {
					stringRoles[i] = s
				}
			}
			ctx.SetRoles(stringRoles)
		}
		
		ctx.SetTokenClaims(claims)
		ctx.Next()
	}
}

// RequireRole 要求特定角色
func RequireRole(role string) context.HandlerFunc {
	return func(ctx *context.Context) {
		if !ctx.HasRole(role) {
			ctx.AbortWithStatusJSON(http.StatusForbidden, context.H{
				"error": "Insufficient permissions",
				"required_role": role,
			})
			return
		}
		ctx.Next()
	}
}

func parseJWT(tokenString, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	
	if err != nil {
		return nil, err
	}
	
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	
	return nil, jwt.ErrSignatureInvalid
}
`
const userModelContent = `user model`
const userServiceContent = `user service`
const userValidatorContent = `user validator`
const authServiceContent = `package services
import (
	"errors"
	"time"
	
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	
	"{{.ProjectName}}/app/models"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user account is disabled")
)

// AuthService 認證服務
type AuthService struct {
	db     *gorm.DB
	secret string
}

// NewAuthService 創建認證服務
func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{
		db:     db,
		secret: "your-secret-key", // 應從配置讀取
	}
}

// Login 用戶登入
func (s *AuthService) Login(username, password string) (*models.User, string, error) {
	var user models.User
	
	err := s.db.Where("username = ? OR email = ?", username, username).
		Preload("Roles").
		First(&user).Error
		
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrInvalidCredentials
		}
		return nil, "", err
	}
	
	// 驗證密碼
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, "", ErrInvalidCredentials
	}
	
	// 檢查用戶狀態
	if !user.IsActive {
		return nil, "", ErrUserDisabled
	}
	
	// 生成 JWT
	token, err := s.generateToken(&user)
	if err != nil {
		return nil, "", err
	}
	
	user.Password = ""
	return &user, token, nil
}

// Register 用戶註冊
func (s *AuthService) Register(req models.RegisterRequest) (*models.User, string, error) {
	// 檢查用戶是否存在
	var count int64
	s.db.Model(&models.User{}).
		Where("username = ? OR email = ?", req.Username, req.Email).
		Count(&count)
		
	if count > 0 {
		return nil, "", ErrDuplicate
	}
	
	// 加密密碼
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	
	// 創建用戶
	user := &models.User{
		Username:  req.Username,
		Email:     req.Email,
		Password:  string(hashedPassword),
		FirstName: req.FirstName,
		LastName:  req.LastName,
		IsActive:  true,
	}
	
	// 分配默認角色
	var userRole models.Role
	if err := s.db.Where("name = ?", "user").First(&userRole).Error; err == nil {
		user.Roles = []models.Role{userRole}
	}
	
	if err := s.db.Create(user).Error; err != nil {
		return nil, "", err
	}
	
	// 生成 token
	token, err := s.generateToken(user)
	if err != nil {
		return nil, "", err
	}
	
	user.Password = ""
	return user, token, nil
}

func (s *AuthService) generateToken(user *models.User) (string, error) {
	roles := make([]string, len(user.Roles))
	for i, role := range user.Roles {
		roles[i] = role.Name
	}
	
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"email":    user.Email,
		"roles":    roles,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
		"iss":      "hypgo-api",
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secret))
}
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

# Logs
logs/
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

A high-performance API server built with HypGo framework, featuring HTTP/3 support.

## 🚀 Features

- **HTTP/3 Support** - Built-in QUIC protocol support
- **JWT Authentication** - Secure token-based authentication
- **Rate Limiting** - Configurable request rate limiting
- **Database Support** - PostgreSQL, MySQL, SQLite
- **Redis Caching** - Built-in Redis integration
- **WebSocket Support** - Real-time communication
- **Hot Reload** - Development mode with automatic reloading
- **Docker Ready** - Complete Docker setup

## 📋 Prerequisites

- Go 1.21+
- PostgreSQL or MySQL (optional, can use SQLite)
- Redis (optional)
- Docker & Docker Compose (optional)

## 🛠️ Installation

1. Install dependencies:
` + "```bash" + `
go mod tidy
` + "```" + `

2. Copy environment variables:
` + "```bash" + `
cp .env.example .env
` + "```" + `

3. Install development tools:
` + "```bash" + `
make install-tools
` + "```" + `

4. Generate certificates (for HTTP/3):
` + "```bash" + `
make cert
` + "```" + `

## 🚦 Quick Start

### Development Mode
` + "```bash" + `
# Run with hot reload
make dev
` + "```" + `

### Production Mode
` + "```bash" + `
# Build binary
make build

# Run binary
./bin/hypgo-api
` + "```" + `

### Docker
` + "```bash" + `
# Start all services
make docker-compose-up

# Stop all services
make docker-compose-down
` + "```" + `

## 📡 API Endpoints

### Health Check
- ` + "`GET /health`" + ` - Health check endpoint
- ` + "`GET /metrics`" + ` - Prometheus metrics

### Authentication
- ` + "`POST /api/v1/auth/register`" + ` - User registration
- ` + "`POST /api/v1/auth/login`" + ` - User login
- ` + "`POST /api/v1/auth/refresh`" + ` - Refresh token
- ` + "`POST /api/v1/auth/logout`" + ` - User logout

### Users (Protected)
- ` + "`GET /api/v1/users`" + ` - List users
- ` + "`POST /api/v1/users`" + ` - Create user (admin only)
- ` + "`GET /api/v1/users/:id`" + ` - Get user details
- ` + "`PUT /api/v1/users/:id`" + ` - Update user
- ` + "`DELETE /api/v1/users/:id`" + ` - Delete user (admin only)

### WebSocket
- ` + "`GET /api/v1/ws`" + ` - WebSocket connection

## 🧪 Testing

` + "```bash" + `
# Run tests
make test

# Run with coverage
make test
` + "```" + `

## 📊 Configuration

Edit ` + "`config/config.yaml`" + ` or use environment variables:

` + "```yaml" + `
server:
  protocol: http3    # http1, http2, http3
  addr: :8080

database:
  driver: postgres
  dsn: ${DB_DSN}

logger:
  level: debug
  output: both       # stdout, file, both
` + "```" + `

## 📦 Project Structure

` + "```" + `
.
├── app/
│   ├── controllers/    # Request handlers
│   ├── models/        # Data models
│   ├── services/      # Business logic
│   └── middleware/    # HTTP middleware
├── internal/
│   ├── logger/        # Logger initialization
│   ├── database/      # Database connections
│   └── cache/         # Redis cache
├── config/           # Configuration files
├── migrations/       # Database migrations
├── logs/            # Log files
└── main.go          # Entry point
` + "```" + `

## 📝 License

MIT License`

const goModContent = `module {{.ProjectName}}

go 1.21

require (
	github.com/maoxiaoyue/hypgo v0.1.0
	github.com/golang-jwt/jwt/v5 v5.2.0
	github.com/redis/go-redis/v9 v9.3.0
	github.com/spf13/viper v1.18.2
	golang.org/x/crypto v0.17.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gorm.io/driver/mysql v1.5.2
	gorm.io/driver/postgres v1.5.4
	gorm.io/driver/sqlite v1.5.4
	gorm.io/gorm v1.25.5
)`

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
