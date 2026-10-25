// @chris
package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "hyp",
		Short: "HypGo CLI - AI-Human Collaborative Go Web Framework",
		Long: `HypGo CLI - Modern Go web framework with AI-human collaborative development.

AI Collaboration:
  context        Generate project manifest for AI tools
  ai-rules       Generate config files for Codex, Gemini, Cursor, Copilot, Windsurf
  chkcomment     Check annotation completeness in Go source files
  impact         Analyze change impact before modifying shared packages
  lint           Check Schema completeness from the manifest (CI gate)

Testing:
  observe        List or open contract observe HTML reports
  eval-report    Show contract test quality trend from .hyp/eval_history.jsonl

Project Management:
  new / api      Create full-stack or API-only project
  run            Start with hot reload + AutoSync (.hyp/context.yaml)
  restart        Zero-downtime hot restart (Unix SIGUSR2 via hypgo.pid)
  generate       Generate controller / model / service with Schema + Error Catalog

Database:
  migrate diff      Generate SQL migration from model struct changes
  migrate snapshot  Save current schema as baseline

Deployment:
  health         Check running application health

Use "hyp [command] --help" for detailed information about each command.`,
		Version: Version,
	}
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate(`HypGo CLI {{.Version}}
AI-Human Collaborative Go Framework — Web (HTTP/1.1 + HTTP/2 + HTTP/3) / CLI / Desktop / gRPC
`)
	registerCommands()
}

func registerCommands() {
	// 以下命令各自在 .go 檔案的 init() 中註冊：
	// new.go → newCmd
	// api.go → apiCmd
	// generate.go → generateCmd
	// airules.go → aiRulesCmd
	// version.go → versionCmd
	// health.go → healthCmd

	// run / restart 定義於 run.go、restart.go（Unix）/ restart_windows.go。
	// 先前這裡註冊的是空的 Run stub：hyp run / restart / docker 什麼都不做就 exit 0，
	// 真正的實作從未被註冊；docker 已移除（hyp api 會產生 Dockerfile）
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(chkcommentCmd)
	rootCmd.AddCommand(impactCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(observeCmd)
	rootCmd.AddCommand(evalReportCmd)
}

// Execute 允許其他包執行根命令
func Execute() error {
	return rootCmd.Execute()
}

// AddCommand 允許其他包添加命令
func AddCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}
