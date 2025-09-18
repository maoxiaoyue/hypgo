package main

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	version = "0.2.0"
	rootCmd = &cobra.Command{
		Use:   "hyp",
		Short: "HypGo CLI - A powerful web framework with HTTP/3 support",
		Long: `HypGo CLI is a command-line tool for the HypGo framework.
It helps you create and manage HypGo projects with ease.

Features:
  - HTTP/3 with QUIC support
  - Hot reload development
  - Database migrations
  - Plugin management
  - Docker integration`,
		Version: version,
	}
)

func init() {
	// 設置版本輸出模板
	rootCmd.SetVersionTemplate(`HypGo CLI {{.Version}}
Framework for building high-performance web applications with HTTP/3 support`)
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

	r.Register(PluginMetadata{
		Name:         "redis",
		Version:      "1.0.0",
		Description:  "Redis in-memory data structure store",
		Category:     "Cache",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/redis",
		ConfigFile:   "redis.yaml",
		Dependencies: []string{"github.com/go-redis/redis/v8"},
		Author:       "HypGo Team",
		License:      "MIT",
	})

	r.Register(PluginMetadata{
		Name:         "nats",
		Version:      "1.0.0",
		Description:  "NATS messaging system",
		Category:     "Message Queue",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/nats",
		ConfigFile:   "nats.yaml",
		Dependencies: []string{"github.com/nats-io/nats.go"},
		Author:       "HypGo Team",
		License:      "MIT",
	})

	r.Register(PluginMetadata{
		Name:         "mongodb",
		Version:      "1.0.0",
		Description:  "MongoDB document-oriented database",
		Category:     "NoSQL Database",
		Repository:   "github.com/maoxiaoyue/hypgo/plugins/mongodb",
		ConfigFile:   "mongodb.yaml",
		Dependencies: []string{"go.mongodb.org/mongo-driver"},
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
