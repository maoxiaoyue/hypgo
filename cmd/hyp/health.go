package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check application health status",
	Long:  `Perform a health check on the running HypGo application`,
	RunE:  runHealth,
}

func runHealth(cmd *cobra.Command, args []string) error {
	// 讀取配置獲取端口
	port := "8080"
	if viper.ConfigFileUsed() == "" {
		viper.SetConfigFile("config/config.yaml")
		if err := viper.ReadInConfig(); err == nil {
			addr := viper.GetString("server.addr")
			if addr != "" && len(addr) > 1 {
				port = addr[1:] // 移除 ":" 前綴
			}
		}
	}

	// 構建健康檢查 URL
	url := fmt.Sprintf("http://localhost:%s/api/health", port)

	// 創建 HTTP 客戶端（超時設置）
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	// 發送健康檢查請求
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("❌ Health check failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 檢查響應狀態
	if resp.StatusCode == http.StatusOK {
		fmt.Println("✅ Application is healthy")
		os.Exit(0)
	} else {
		fmt.Printf("❌ Application is unhealthy (status: %d)\n", resp.StatusCode)
		os.Exit(1)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(healthCmd)
}
