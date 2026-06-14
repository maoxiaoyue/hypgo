//go:build windows

// @chris
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the HypGo application gracefully (Not supported on Windows)",
	Long:  `Restart the application with zero downtime using graceful shutdown. This feature is currently not supported on Windows.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("❌ Graceful restart via signals is not supported on Windows.")
		fmt.Println("💡 Please restart the application manually.")
		return nil
	},
}
