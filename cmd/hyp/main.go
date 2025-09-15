package main

import (
	"fmt"
	"os"

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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Execute 允許其他包執行根命令
func Execute() error {
	return rootCmd.Execute()
}

// AddCommand 允許其他包添加命令
func AddCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}
