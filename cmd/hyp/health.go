// @chris
package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// healthDefaultPaths 未指定 --path 時依序嘗試：hyp api 專案為 /health，hyp new（web）專案為 /api/health
var healthDefaultPaths = []string{"/health", "/api/health"}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check application health status",
	Long: `Perform a health check on the running HypGo application.

The address and TLS setting are read from config/config.yaml (server.addr,
server.tls.enabled). An empty host or 0.0.0.0 / :: is checked via localhost.
With TLS enabled the check uses https; certificate verification is skipped for
loopback hosts (self-signed dev certs) or when --insecure is given.

Without --path, /health (hyp api projects) is tried first, then /api/health
(hyp new web projects) when the first returns 404.

Exit codes:
  0   healthy (HTTP 200)
  1   unreachable or non-200

Examples:
  hyp health
  hyp health --path /api/health
  hyp health -c config/prod.yaml --insecure`,
	RunE:         runHealth,
	SilenceUsage: true,
}

func runHealth(cmd *cobra.Command, args []string) error {
	path, _ := cmd.Flags().GetString("path")
	cfgFile, _ := cmd.Flags().GetString("config")
	insecure, _ := cmd.Flags().GetBool("insecure")

	addr, useTLS := ":8080", false
	v := viper.New()
	v.SetConfigFile(cfgFile)
	if err := v.ReadInConfig(); err == nil {
		if a := v.GetString("server.addr"); a != "" {
			addr = a
		}
		useTLS = v.GetBool("server.tls.enabled")
	} else if cmd.Flags().Changed("config") {
		return fmt.Errorf("read %s: %w", cfgFile, err)
	}

	// 先前以 addr[1:] 取埠號：127.0.0.1:9999 會變成 localhost:27.0.0.1:9999；也忽略 TLS
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid server.addr %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	scheme := "http"
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if useTLS {
		scheme = "https"
		if insecure || isLoopbackHost(host) {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: tr}
	base := scheme + "://" + net.JoinHostPort(host, port)

	paths := healthDefaultPaths
	if path != "" {
		paths = []string{path}
	}
	var url string
	var status int
	for _, p := range paths {
		url = base + p
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("health check failed: %w", err)
		}
		resp.Body.Close()
		status = resp.StatusCode
		if status != http.StatusNotFound {
			break
		}
	}

	// 先前直接 os.Exit：成功與失敗都繞過 cobra，defer 不執行
	if status != http.StatusOK {
		return fmt.Errorf("application is unhealthy: GET %s returned %d", url, status)
	}
	fmt.Printf("✅ Application is healthy (%s)\n", url)
	return nil
}

// isLoopbackHost 判斷是否為本機位址（localhost 或 loopback IP）
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func init() {
	healthCmd.Flags().String("path", "", "Health endpoint path (default: try /health, then /api/health)")
	healthCmd.Flags().StringP("config", "c", "config/config.yaml", "Config file to read server.addr / server.tls from")
	healthCmd.Flags().Bool("insecure", false, "Skip TLS certificate verification for non-loopback hosts")
	rootCmd.AddCommand(healthCmd)
}
