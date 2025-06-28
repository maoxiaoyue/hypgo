package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hyp",
	Short: "hypgo framework CLI tool",
	Long:  `hypgo is a modern Go web framework with HTTP/3 support`,
}

func init() {
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(dockerCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
