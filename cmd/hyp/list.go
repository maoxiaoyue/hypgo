// cmd/hyp/list.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// PluginInfo 插件資訊
type PluginInfo struct {
	Name        string
	Description string
	Version     string
	Repository  string
	ConfigFile  string
	Installed   bool
	Enabled     bool
	Category    string
}

// 所有可用的插件列表
var availablePlugins = []PluginInfo{
	// 消息隊列
	{
		Name:        "rabbitmq",
		Description: "RabbitMQ message queue support",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/rabbitmq",
		ConfigFile:  "rabbitmq.yaml",
		Category:    "Message Queue",
	},
	{
		Name:        "kafka",
		Description: "Apache Kafka distributed streaming platform",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/kafka",
		ConfigFile:  "kafka.yaml",
		Category:    "Message Queue",
	},
	{
		Name:        "nats",
		Description: "NATS messaging system",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/nats",
		ConfigFile:  "nats.yaml",
		Category:    "Message Queue",
	},
	{
		Name:        "zeromq",
		Description: "ZeroMQ high-performance messaging library",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/zeromq",
		ConfigFile:  "zeromq.yaml",
		Category:    "Message Queue",
	},

	// NoSQL 資料庫
	{
		Name:        "cassandra",
		Description: "Apache Cassandra distributed NoSQL database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/cassandra",
		ConfigFile:  "cassandra.yaml",
		Category:    "NoSQL Database",
	},
	{
		Name:        "scylladb",
		Description: "ScyllaDB high-performance Cassandra-compatible database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/scylladb",
		ConfigFile:  "scylladb.yaml",
		Category:    "NoSQL Database",
	},
	{
		Name:        "mongodb",
		Description: "MongoDB document-oriented database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/mongodb",
		ConfigFile:  "mongodb.yaml",
		Category:    "NoSQL Database",
	},

	// 搜尋引擎
	{
		Name:        "elasticsearch",
		Description: "Elasticsearch search and analytics engine",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/elasticsearch",
		ConfigFile:  "elasticsearch.yaml",
		Category:    "Search Engine",
	},

	// 快取
	{
		Name:        "redis",
		Description: "Redis in-memory data structure store",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/redis",
		ConfigFile:  "redis.yaml",
		Category:    "Cache",
	},
	{
		Name:        "memcached",
		Description: "Memcached distributed memory caching system",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/memcached",
		ConfigFile:  "memcached.yaml",
		Category:    "Cache",
	},

	// SQL 資料庫
	{
		Name:        "mysql",
		Description: "MySQL relational database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/mysql",
		ConfigFile:  "mysql.yaml",
		Category:    "SQL Database",
	},
	{
		Name:        "postgresql",
		Description: "PostgreSQL advanced open source database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/postgresql",
		ConfigFile:  "postgresql.yaml",
		Category:    "SQL Database",
	},
	{
		Name:        "tidb",
		Description: "TiDB distributed SQL database",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/tidb",
		ConfigFile:  "tidb.yaml",
		Category:    "SQL Database",
	},

	// 監控和追蹤
	{
		Name:        "prometheus",
		Description: "Prometheus monitoring and alerting toolkit",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/prometheus",
		ConfigFile:  "prometheus.yaml",
		Category:    "Monitoring",
	},
	{
		Name:        "jaeger",
		Description: "Jaeger distributed tracing system",
		Version:     "1.0.0",
		Repository:  "github.com/maoxiaoyue/hypgo/plugins/jaeger",
		ConfigFile:  "jaeger.yaml",
		Category:    "Monitoring",
	},
}

// listCmd 代表 list 命令
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available plugins",
	Long: `List all available plugins that can be installed in HypGo framework.
	
This command shows:
  - Plugin name and description
  - Installation status
  - Version information
  - Category

Examples:
  hyp list                    # List all plugins
  hyp list --installed        # List only installed plugins
  hyp list --category cache   # List plugins by category`,
	Run: runList,
}

var (
	showInstalled bool
	category      string
	verbose       bool
)

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().BoolVarP(&showInstalled, "installed", "i", false, "Show only installed plugins")
	listCmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category")
	listCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information")
}

func runList(cmd *cobra.Command, args []string) {
	// 檢查是否在 HypGo 項目中
	projectInfo := checkProject()

	// 更新插件的安裝狀態
	updatePluginStatus(projectInfo)

	// 過濾插件列表
	plugins := filterPlugins(showInstalled, category)

	if len(plugins) == 0 {
		fmt.Println("No plugins found matching the criteria.")
		return
	}

	// 顯示插件列表
	if verbose {
		displayVerboseList(plugins)
	} else {
		displaySimpleList(plugins)
	}
}

// checkProject 檢查當前是否在 HypGo 項目中
func checkProject() *ProjectInfo {
	if _, err := os.Stat("go.mod"); err != nil {
		return nil
	}

	// 讀取 go.mod 獲取項目名稱
	content, err := os.ReadFile("go.mod")
	if err != nil {
		return nil
	}

	// 簡單解析 module 名稱
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") {
			moduleName := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			return &ProjectInfo{
				Name:       filepath.Base(moduleName),
				ModuleName: moduleName,
				Path:       ".",
			}
		}
	}

	return nil
}

// ProjectInfo 項目資訊
type ProjectInfo struct {
	Name       string
	ModuleName string
	Path       string
}

// updatePluginStatus 更新插件的安裝狀態
func updatePluginStatus(projectInfo *ProjectInfo) {
	if projectInfo == nil {
		return
	}

	for i := range availablePlugins {
		// 檢查配置文件是否存在
		configPath := filepath.Join("config", availablePlugins[i].ConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			availablePlugins[i].Installed = true

			// 檢查主文件是否存在
			mainFile := filepath.Join("main", availablePlugins[i].Name+".go")
			if _, err := os.Stat(mainFile); err == nil {
				availablePlugins[i].Enabled = true
			}
		}
	}
}

// filterPlugins 過濾插件列表
func filterPlugins(onlyInstalled bool, filterCategory string) []PluginInfo {
	var filtered []PluginInfo

	for _, plugin := range availablePlugins {
		// 過濾已安裝
		if onlyInstalled && !plugin.Installed {
			continue
		}

		// 過濾類別
		if filterCategory != "" &&
			!strings.EqualFold(plugin.Category, filterCategory) {
			continue
		}

		filtered = append(filtered, plugin)
	}

	return filtered
}

// displaySimpleList 顯示簡單列表
func displaySimpleList(plugins []PluginInfo) {
	// 建立表格
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// 表頭
	fmt.Fprintln(w, "NAME\tCATEGORY\tSTATUS\tDESCRIPTION")
	fmt.Fprintln(w, "----\t--------\t------\t-----------")

	// 按類別分組
	categories := make(map[string][]PluginInfo)
	for _, plugin := range plugins {
		categories[plugin.Category] = append(categories[plugin.Category], plugin)
	}

	// 顏色定義
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	// 按類別顯示
	for _, categoryPlugins := range categories {
		for _, plugin := range categoryPlugins {
			status := gray("Not Installed")
			if plugin.Installed {
				if plugin.Enabled {
					status = green("✓ Enabled")
				} else {
					status = yellow("○ Installed")
				}
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				plugin.Name,
				blue(plugin.Category),
				status,
				plugin.Description,
			)
		}
	}

	w.Flush()

	// 顯示統計
	fmt.Println()
	installedCount := 0
	for _, p := range plugins {
		if p.Installed {
			installedCount++
		}
	}
	fmt.Printf("Total: %d plugins (%d installed)\n",
		len(plugins), installedCount)

	// 顯示提示
	fmt.Println()
	fmt.Println("Use 'hyp install <plugin-name>' to install a plugin")
	fmt.Println("Use 'hyp list -v' for detailed information")
}

// displayVerboseList 顯示詳細列表
func displayVerboseList(plugins []PluginInfo) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()

	// 按類別分組
	categories := make(map[string][]PluginInfo)
	for _, plugin := range plugins {
		categories[plugin.Category] = append(categories[plugin.Category], plugin)
	}

	// 顯示每個類別
	for category, categoryPlugins := range categories {
		fmt.Printf("\n%s\n", blue("【"+category+"】"))
		fmt.Println(strings.Repeat("-", 80))

		for _, plugin := range categoryPlugins {
			// 狀態圖標
			statusIcon := "◯"
			statusText := gray("Not Installed")
			if plugin.Installed {
				if plugin.Enabled {
					statusIcon = green("✓")
					statusText = green("Enabled")
				} else {
					statusIcon = yellow("◐")
					statusText = yellow("Installed")
				}
			}

			fmt.Printf("%s %s (%s)\n", statusIcon, cyan(plugin.Name), plugin.Version)
			fmt.Printf("  Status:      %s\n", statusText)
			fmt.Printf("  Description: %s\n", plugin.Description)
			fmt.Printf("  Repository:  %s\n", gray(plugin.Repository))
			fmt.Printf("  Config File: config/%s\n", plugin.ConfigFile)

			if plugin.Installed {
				fmt.Printf("  Location:    main/%s.go\n", plugin.Name)
			}

			fmt.Println()
		}
	}

	// 顯示安裝指令範例
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("Installation Examples:")
	fmt.Println()
	fmt.Println("  hyp install elasticsearch  # Install Elasticsearch plugin")
	fmt.Println("  hyp install kafka         # Install Kafka plugin")
	fmt.Println("  hyp remove elasticsearch  # Remove Elasticsearch plugin")
	fmt.Println()
}
