package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "0.8.5"
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
	// 以下命令各自在 .go 檔案的 init() 中註冊：
	// new.go → newCmd
	// api.go → apiCmd
	// generate.go → generateCmd
	// airules.go → aiRulesCmd
	// version.go → versionCmd
	// health.go → healthCmd

	// 以下命令定義在 registerCommands 中（尚無獨立 .go 檔案）
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

	rootCmd.AddCommand(&cobra.Command{
		Use:   "docker",
		Short: "Build Docker image for the project",
		Long: `Build a Docker image for the current HypGo project using a
multi-stage Dockerfile based on config.yaml settings.

Examples:
  hyp docker`,
		Run: func(cmd *cobra.Command, args []string) {},
	})

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
