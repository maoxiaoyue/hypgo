package main

import (
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	version = "0.8.0"
	rootCmd = &cobra.Command{
		Use:   "hyp",
		Short: "HypGo CLI - AI-Human Collaborative Go Web Framework",
		Long: `HypGo CLI - Modern Go web framework with AI-human collaborative development.

AI Collaboration:
  context        Generate project manifest for AI tools (~500 tokens vs ~5,000)
  ai-rules       Generate config files for Codex, Gemini, Cursor, Copilot, Windsurf
  chkcomment     Check annotation completeness in Go source files
  impact         Analyze change impact before modifying shared packages

Project Management:
  new / api      Create full-stack or API-only project
  run            Start with hot reload + AutoSync (.hyp/context.yaml)
  restart        Zero-downtime hot restart (Unix SIGUSR2)
  generate       Generate controller / model / service with Schema + Error Catalog

Database:
  migrate diff      Generate SQL migration from model struct changes
  migrate snapshot  Save current schema as baseline

Deployment:
  docker         Build Docker image
  health         Check running application health

Use "hyp [command] --help" for detailed information about each command.`,
		Version: version,
	}
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate(`HypGo CLI {{.Version}}
AI-Human Collaborative Go Web Framework (HTTP/1.1 + HTTP/2 + HTTP/3)
`)
	registerCommands()
}

func registerCommands() {
	// new 命令（new.go 沒有自己的 init 註冊）
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

Examples:
  hyp new myapp
  hyp new my-web-service`,
		Args: cobra.ExactArgs(1),
		Run:  func(cmd *cobra.Command, args []string) {},
	})

	// api: 已在 api.go init() 中註冊
	// list: 已在 list.go init() 中註冊
	// version: 已在 version.go init() 中註冊
	// health: 已在 health.go init() 中註冊
	// ai-rules: 已在 airules.go init() 中註冊

	// run 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the HypGo application",
		Long: `Start the HypGo application in development mode with hot reload.

On startup, AutoSync automatically generates .hyp/context.yaml with the
current project manifest for AI tool consumption.

Examples:
  hyp run`,
		Run: func(cmd *cobra.Command, args []string) {},
	})

	// restart 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Hot restart the application",
		Long: `Perform a zero-downtime hot restart of the running HypGo application.
Sends SIGUSR2 signal, forks a new process, then gracefully shuts down.
Note: NOT supported on Windows.

Examples:
  hyp restart`,
		Run: func(cmd *cobra.Command, args []string) {},
	})

	// docker 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "docker",
		Short: "Build Docker image for the project",
		Long: `Build a Docker image for the current HypGo project using a
multi-stage Dockerfile based on config.yaml settings.

Examples:
  hyp docker`,
		Run: func(cmd *cobra.Command, args []string) {},
	})

	// generate 命令
	rootCmd.AddCommand(&cobra.Command{
		Use:   "generate [type] [name]",
		Short: "Generate code for controllers, models, or services",
		Long: `Generate boilerplate code that follows HypGo conventions.
Generated code integrates Schema-first routes and Typed Error Catalog.

Available types: controller, model, service

Examples:
  hyp generate controller user
  hyp generate model order
  hyp generate service payment`,
		Args: cobra.MinimumNArgs(2),
		Run:  func(cmd *cobra.Command, args []string) {},
	})

	// context, migrate, chkcomment, impact: 各有自己的 .go 檔案定義 var xxxCmd
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(chkcommentCmd)
	rootCmd.AddCommand(impactCmd)
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
