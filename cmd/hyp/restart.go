//go:build !windows

// @chris
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// restartPIDFile server.Start() 寫入 CWD 的 PID 檔（pkg/server pidFilePath）
const restartPIDFile = "hypgo.pid"

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the HypGo application gracefully (Unix only)",
	Long: `Restart the running application with zero downtime.

Reads the PID from hypgo.pid (written by server.Start() in the app's working
directory, so run this command from that directory) and sends SIGUSR2 to that
process only. The server forks a new process that inherits the listener, then
the old process shuts down gracefully.

Requires server.enable_graceful_restart: true in config/config.yaml.
Not supported on Windows.`,
	RunE:         runRestart,
	SilenceUsage: true,
}

func runRestart(cmd *cobra.Command, args []string) error {
	// 先前 PID 檔不存在時改以 pgrep -f main.go 找行程：會比對到編輯器等無關行程，
	// 再送 SIGUSR2（預設動作是結束行程）把它們殺掉。現在只認 hypgo.pid
	pid, err := readPIDFile(restartPIDFile)
	if err != nil {
		return err
	}
	if !isProcessRunning(pid) {
		return fmt.Errorf("process %d from %s is not running (stale PID file); start the app first", pid, restartPIDFile)
	}

	// 未啟用 graceful restart 時 server 不處理 SIGUSR2，訊號的預設動作會直接結束行程
	if enabled, ok := gracefulRestartConfigured("config/config.yaml"); ok && !enabled {
		return fmt.Errorf("server.enable_graceful_restart is false in config/config.yaml; SIGUSR2 would terminate PID %d instead of restarting it", pid)
	}

	fmt.Printf("🔄 Sending graceful restart signal (SIGUSR2) to PID %d...\n", pid)
	if err := syscall.Kill(pid, syscall.SIGUSR2); err != nil {
		return fmt.Errorf("failed to send signal to PID %d: %w", pid, err)
	}

	// 新行程啟動時（server.Start()）會覆寫 PID 檔
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if newPid, err := readPIDFile(restartPIDFile); err == nil && newPid != pid && isProcessRunning(newPid) {
			fmt.Printf("✅ Application restarted successfully! New PID: %d\n", newPid)
			return nil
		}
	}
	return fmt.Errorf("no new process recorded in %s within 10s; check the application logs", restartPIDFile)
}

// readPIDFile 讀取 PID 檔
func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%s not found in the current directory: start the app (hyp run / go run .) from here first", path)
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in %s: %q", path, strings.TrimSpace(string(data)))
	}
	return pid, nil
}

// gracefulRestartConfigured 讀取設定檔的 server.enable_graceful_restart；ok 為 false 表示無法判斷
func gracefulRestartConfigured(path string) (enabled, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var cfg struct {
		Server struct {
			EnableGracefulRestart *bool `yaml:"enable_graceful_restart"`
		} `yaml:"server"`
	}
	if yaml.Unmarshal(data, &cfg) != nil || cfg.Server.EnableGracefulRestart == nil {
		return false, false
	}
	return *cfg.Server.EnableGracefulRestart, true
}

func isProcessRunning(pid int) bool {
	// 送訊號 0 檢查行程是否存在
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}
