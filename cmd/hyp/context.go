package main

import (
	"fmt"
	"os"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Generate project manifest for AI collaboration",
	Long: `Generate a machine-readable project manifest (YAML or JSON) describing
routes, middleware, configuration, and schema metadata.

The manifest enables AI tools to understand the project structure with
minimal tokens (~500 tokens vs ~5,000 for reading source files).

Output includes:
  - Server configuration (addr, protocol, TLS)
  - All registered routes with method, path, and handler names
  - Schema metadata (Input/Output types, summary, tags) if available
  - Database configuration (driver, replicas)
  - Middleware stack

The manifest is also auto-generated on Server.Start() via AutoSync
and saved to .hyp/context.yaml. This command allows you to generate
it manually or in a different format.

Flags:
  -o, --output   Output file path (default: stdout)
  -f, --format   Output format: yaml or json (default: yaml)

Examples:
  hyp context                              Print YAML to stdout
  hyp context -f json                      Print JSON to stdout
  hyp context -o .hyp/manifest.yaml        Save YAML to file
  hyp context -o manifest.json -f json     Save JSON to file`,
	RunE: runContext,
}

func init() {
	contextCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	contextCmd.Flags().StringP("format", "f", "yaml", "Output format: yaml or json")
}

func runContext(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	format, _ := cmd.Flags().GetString("format")

	// 載入設定（若存在）
	var cfg *config.Config
	configPath := "app/config/config.yaml"
	if _, err := os.Stat(configPath); err == nil {
		cfg = &config.Config{}
		loader := config.NewConfigLoader(configPath)
		if err := loader.Load(configPath, cfg); err != nil {
			cfg = nil // 載入失敗則不含設定資訊
		}
	}

	// 建立 router（目前無法自動掃描使用者路由，輸出基礎結構）
	r := router.New()

	// 收集 manifest
	c := manifest.NewCollector(r, cfg)
	m := c.Collect()

	// 輸出
	if output != "" {
		if err := manifest.SaveToFile(output, m, format); err != nil {
			return fmt.Errorf("failed to save manifest: %w", err)
		}
		fmt.Printf("Manifest saved to %s\n", output)
		return nil
	}

	// 輸出到 stdout
	switch format {
	case "json":
		return manifest.WriteJSON(os.Stdout, m)
	default:
		return manifest.WriteYAML(os.Stdout, m)
	}
}
