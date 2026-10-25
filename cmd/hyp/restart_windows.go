//go:build windows

// @chris
package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the HypGo application gracefully (Not supported on Windows)",
	Long:  `Restart the application with zero downtime using graceful shutdown. This feature is not supported on Windows.`,
	// 先前印出訊息後回傳 nil（exit 0），腳本會誤以為重啟成功
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("graceful restart (SIGUSR2 + listener inheritance) is Unix-only; on Windows stop the app and start it again (hyp run restarts it on file changes)")
	},
	SilenceUsage: true,
}
