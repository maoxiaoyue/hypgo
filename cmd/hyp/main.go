package main

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	version = "0.4.0"
	rootCmd = &cobra.Command{
		Use:   "hyp",
		Short: "HypGo CLI - Modern Go web framework with AI collaboration",
		Long: `HypGo CLI is a command-line tool for the HypGo framework.

HypGo is a modern Go web framework with native HTTP/1.1, HTTP/2, HTTP/3 (QUIC)
support and built-in AI-human collaborative development toolchain.

Project Management:
  new            Create a full-stack project (with frontend templates)
  api            Create an API-only project
  run            Start the application with hot reload
  restart        Zero-downtime hot restart (Unix SIGUSR2)

AI Collaboration:
  context        Generate project manifest (YAML/JSON) for AI tools
  ai-rules       Generate configuration files for AI coding tools
  chkcomment     Check annotation completeness in Go source files
  impact         Analyze change impact before modifying shared packages

Code Generation:
  generate       Generate boilerplate code (controller, model, service)

Database:
  migrate        Generate SQL migrations from model struct changes

Deployment:
  docker         Build Docker image
  health         Check running application health

Other:
  list           List available plugins

Use "hyp [command] --help" for detailed information about each command.`,
		Version: version,
	}
)

func init() {
	// 設置版本輸出模板
	rootCmd.SetVersionTemplate(`HypGo CLI {{.Version}}
Framework for building high-performance web applications with HTTP/3 support
`)
	// 註冊所有命令
	registerCommands()
}

// registerCommands 註冊所有可用的命令
func registerCommands() {
	// 註冊 new 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "new [project-name]",
		Short: "Create a new HypGo project",
		Long: `Create a new full-stack HypGo project with complete MVC directory structure.

Generated structure:
  myapp/
  ├── app/
  │   ├── controllers/   HTTP request handlers
  │   ├── models/        Database models (Bun ORM)
  │   ├── services/      Business logic layer
  │   └── config/        config.yaml (server, database, logger)
  ├── public/            Static files (CSS, JS, images)
  ├── views/             HTML templates (welcome page included)
  ├── main.go            Application entry point
  ├── go.mod             Go module definition
  └── Dockerfile         Docker build configuration

After creation:
  cd myapp && go mod tidy && hyp run

For an API-only project (no static files or templates), use "hyp api" instead.

Examples:
  hyp new myapp
  hyp new my-web-service`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 new.go 中的實際實作
			// RunNew(args[0])
		},
	})

	// 註冊 api 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "api [project-name]",
		Short: "Create a new API-only project",
		Long: `Create a new HypGo API-only project without static files, templates, or views.

This is the recommended starting point for microservices, REST APIs, and
backend services that don't serve HTML pages.

Generated structure:
  myapi/
  ├── app/
  │   ├── controllers/   HTTP request handlers
  │   ├── models/        Database models (Bun ORM)
  │   ├── services/      Business logic layer
  │   └── config/        config.yaml
  ├── main.go            Application entry point
  ├── go.mod             Go module definition
  └── Dockerfile         Docker build configuration

Compared to "hyp new":
  - No public/ directory (no static files)
  - No views/ directory (no HTML templates)
  - Leaner project structure focused on API endpoints

After creation:
  cd myapi && go mod tidy && hyp run

Examples:
  hyp api myapi
  hyp api user-service`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 api.go 中的實際實作
			// RunAPI(args[0])
		},
	})

	// 註冊 run 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the HypGo application",
		Long: `Start the HypGo application in development mode with hot reload.

In development mode, the file watcher monitors your source files and
automatically rebuilds and restarts the server when changes are detected.
A change summary is displayed after each reload:

  === Change Summary [15:04:05] ===
    Modified (2):
      ~ app/controllers/user.go
      ~ app/models/user.go
    Total: 2 changes

On startup, AutoSync automatically generates .hyp/context.yaml with the
current project manifest (routes, types, config) for AI tool consumption.

The server reads configuration from app/config/config.yaml, which controls
protocol (http1/http2/http3/auto), TLS, database, logger, and more.

Examples:
  hyp run`,
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 run.go 中的實際實作
			// RunServer()
		},
	})

	// 註冊 list 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all available plugins",
		Long: `List all available HypGo plugins that can be installed.

Displays each plugin's name, version, description, and category.
Available categories include Search Engine, Message Queue, and NoSQL Database.

Currently available plugins:
  elasticsearch  Elasticsearch search and analytics engine
  kafka          Apache Kafka distributed streaming platform
  cassandra      Apache Cassandra distributed NoSQL database
  rabbitmq       RabbitMQ message queue support

Plugins are configured via their own YAML config files (e.g., kafka.yaml)
placed in the project's config directory.

Examples:
  hyp list`,
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 list 功能
			// RunList()
		},
	})

	// 註冊 restart 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Hot restart the application",
		Long: `Perform a zero-downtime hot restart of the running HypGo application.

On Unix systems, this sends a SIGUSR2 signal to the running process, which
triggers the graceful restart sequence:

  1. Fork a new child process, passing the listening socket FD
  2. Child starts and begins accepting new connections
  3. Parent stops accepting new connections
  4. Parent waits for in-flight requests to complete (with timeout)
  5. Parent exits cleanly

During the restart, no connections are dropped — the old and new processes
overlap briefly to ensure continuous service.

Note: This command is NOT supported on Windows.

Examples:
  hyp restart`,
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 restart.go 中的實際實作
			// RunRestart()
		},
	})

	// 註冊 docker 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "docker",
		Short: "Build Docker image for the project",
		Long: `Build a Docker image for the current HypGo project.

Uses a multi-stage Dockerfile to produce a minimal production image:

  Stage 1 (builder): Compiles the Go binary with CGO_DISABLED=1
  Stage 2 (runtime): Copies the binary into a scratch/alpine image

The image includes:
  - Compiled Go binary
  - config.yaml (from app/config/)
  - TLS certificates (if configured)
  - Static files and templates (for full-stack projects)

Image settings (name, tag, base image) are read from app/config/config.yaml.

Examples:
  hyp docker`,
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 docker.go 中的實際實作
			// RunDocker()
		},
	})

	// 註冊 generate 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "generate [type] [name]",
		Short: "Generate code for controllers, models, or services",
		Long: `Generate boilerplate code that follows HypGo conventions.

The generated code automatically integrates with HypGo's AI collaboration
toolchain, including Schema-first routes and Typed Error Catalog.

Available types:
  controller    Generate a controller with Schema-first route registration
                and structured error handling (errors.Define)
  model         Generate a Bun ORM model with bun struct tags
  service       Generate a service layer with Error Catalog integration

Generated file locations:
  controller → app/controllers/<name>.go
  model      → app/models/<name>.go
  service    → app/services/<name>.go

The generated controller includes:
  - router.Schema() registration with Input/Output types
  - Typed error definitions (errors.Define)
  - Standard CRUD handler stubs

Examples:
  hyp generate controller user
  hyp generate model order
  hyp generate service payment`,
		Args: cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 generate.go 中的實際實作
			// RunGenerate(args[0], args[1])
		},
	})

	// 註冊 context 命令（AI 協作用 manifest 生成）
	rootCmd.AddCommand(contextCmd)

	// 註冊 migrate 命令（Migration Diff CLI）
	rootCmd.AddCommand(migrateCmd)

	// 註冊 chkcomment 命令（Annotation Protocol 檢查）
	rootCmd.AddCommand(chkcommentCmd)

	// 註冊 impact 命令（Change Impact Analysis）
	rootCmd.AddCommand(impactCmd)

	// 註冊 ai-rules 命令（跨 AI 工具配置檔生成）
	rootCmd.AddCommand(aiRulesCmd)

	// 註冊 health 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "health",
		Short: "Check application health status",
		Long: `Check the health status of the running HypGo application.

Sends a request to the application's health endpoint and reports:
  - Server status (running / shutting down / unreachable)
  - Protocol in use (HTTP/1.1, HTTP/2, HTTP/3)
  - Uptime
  - Active connections

This command is also used internally by "hyp restart" to verify the new
process is ready before shutting down the old one.

Examples:
  hyp health`,
		Run: func(cmd *cobra.Command, args []string) {
			// 這裡應該呼叫 health.go 中的實際實作
			// RunHealth()
		},
	})
}

// Execute 允許其他包執行根命令
func Execute() error {
	return rootCmd.Execute()
}

// AddCommand 允許其他包添加命令
func AddCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}

// PluginRegistry 全局插件註冊表
type PluginRegistry struct {
	plugins map[string]PluginMetadata
	mu      sync.RWMutex
}

// PluginMetadata 插件元數據
type PluginMetadata struct {
	Name         string
	Version      string
	Description  string
	Category     string
	Repository   string
	ConfigFile   string
	Dependencies []string
	Author       string
	License      string
}

var (
	registry *PluginRegistry
	once     sync.Once
)

// GetRegistry 獲取全局插件註冊表
func GetRegistry() *PluginRegistry {
	once.Do(func() {
		registry = &PluginRegistry{
			plugins: make(map[string]PluginMetadata),
		}
		registry.initialize()
	})
	return registry
}

// initialize 初始化插件註冊表
func (r *PluginRegistry) initialize() {
	// 註冊所有可用插件
	r.Register(PluginMetadata{
		Name:        "elasticsearch",
		Version:     "1.0.0",
		Description: "Elasticsearch search and analytics engine",
		Category:    "Search Engine",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/elasticsearch",
		ConfigFile:  "elasticsearch.yaml",
		Author:      "HypGo Team",
		License:     "MIT",
	})

	r.Register(PluginMetadata{
		Name:         "kafka",
		Version:      "1.0.0",
		Description:  "Apache Kafka distributed streaming platform",
		Category:     "Message Queue",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/kafka",
		ConfigFile:   "kafka.yaml",
		Dependencies: []string{"github.com/Shopify/sarama"},
		Author:       "HypGo Team",
		License:      "MIT",
	})

	r.Register(PluginMetadata{
		Name:         "cassandra",
		Version:      "1.0.0",
		Description:  "Apache Cassandra distributed NoSQL database",
		Category:     "NoSQL Database",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/cassandra",
		ConfigFile:   "cassandra.yaml",
		Dependencies: []string{"github.com/gocql/gocql"},
		Author:       "HypGo Team",
		License:      "MIT",
	})

	r.Register(PluginMetadata{
		Name:         "rabbitmq",
		Version:      "1.0.0",
		Description:  "RabbitMQ message queue support",
		Category:     "Message Queue",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/rabbitmq",
		ConfigFile:   "rabbitmq.yaml",
		Dependencies: []string{"github.com/streadway/amqp"},
		Author:       "HypGo Team",
		License:      "MIT",
	})
}

// Register 註冊插件
func (r *PluginRegistry) Register(metadata PluginMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[metadata.Name] = metadata
}

// Get 獲取插件元數據
func (r *PluginRegistry) Get(name string) (PluginMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	metadata, ok := r.plugins[name]
	return metadata, ok
}

// List 列出所有插件
func (r *PluginRegistry) List() []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]PluginMetadata, 0, len(r.plugins))
	for _, metadata := range r.plugins {
		list = append(list, metadata)
	}
	return list
}

// ListByCategory 按類別列出插件
func (r *PluginRegistry) ListByCategory(category string) []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []PluginMetadata
	for _, metadata := range r.plugins {
		if strings.EqualFold(metadata.Category, category) {
			list = append(list, metadata)
		}
	}
	return list
}

// Categories 獲取所有類別
func (r *PluginRegistry) Categories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categoryMap := make(map[string]bool)
	for _, metadata := range r.plugins {
		categoryMap[metadata.Category] = true
	}

	categories := make([]string, 0, len(categoryMap))
	for category := range categoryMap {
		categories = append(categories, category)
	}
	return categories
}
