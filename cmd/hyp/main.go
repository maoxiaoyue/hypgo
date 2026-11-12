// @chris
package main

import (
	"fmt"
	"os"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "hyp",
		Short: "HypGo CLI - AI-Human Collaborative Go Web Framework",
		Long: `HypGo CLI - Modern Go web framework with AI-human collaborative development.

AI Collaboration:
  context        Generate project manifest for AI tools
  ai-rules       Generate rule files for AI coding tools (AGENTS.md, CLAUDE.md, Cursor, Gemini, ...)
  chkcomment     Check annotation completeness in Go source files
  impact         Analyze change impact before modifying shared packages
  lint           Check Schema completeness from the manifest (CI gate)
  diff-log       Log uncommitted changes to logs/ai.diff_YYYYMMDD.log

Testing:
  test           Generate missing test stubs, then run go test
  testgen        Generate missing *_test.go stubs
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
  secret         Encrypt credentials into key.secret (${secret:name} in config.yaml)
  crypt          Encrypt the user/password under a config.yaml key into key.secret

Use "hyp [command] --help" for detailed information about each command.`,
		Version: Version,
		// 執行期錯誤（go test 失敗、檔案已存在…）不附整段 usage：先前約半數命令會印，蓋掉真正的錯誤訊息
		SilenceUsage: true,
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
	// 生成專案的 go.mod 一律 require 與 CLI 相同的 hypgo 版本
	scaffold.HypgoVersion = "v" + Version
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
	rootCmd.AddCommand(secretCmd)
	rootCmd.AddCommand(cryptCmd)
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(chkcommentCmd)
	rootCmd.AddCommand(impactCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(observeCmd)
	rootCmd.AddCommand(evalReportCmd)
}

// requireSubcommand 是只有子命令的父命令（migrate、secret）的 RunE。
// 先前這類命令沒有 Run：hyp migrate dif 這種打錯的子命令只印說明並 exit 0，CI 腳本會當成成功
func requireSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		msg += fmt.Sprintf("\n\nDid you mean this?\n\t%s", s[0])
	}
	return fmt.Errorf("%s\n\nRun '%s --help' for usage", msg, cmd.CommandPath())
}
