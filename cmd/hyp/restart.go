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

Requires server.enable_graceful_restart: true (config file or
HYPGO_SERVER_ENABLE_GRACEFUL_RESTART). When it is not set the server does not
handle SIGUSR2 and the signal would terminate it, so the command refuses;
--force sends the signal anyway (for apps that enable it in code).

This is for a compiled binary. Do not use it on an app started by hyp run or
go run: the new process leaves their control, and their temporary binary is
deleted when they exit.

Not supported on Windows.`,
	Args:         cobra.NoArgs,
	RunE:         runRestart,
	SilenceUsage: true,
}

func init() {
	restartCmd.Flags().Bool("force", false, "Send SIGUSR2 even when the config does not show graceful restart as enabled")
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
	if err := checkPIDWorksHere(pid); err != nil {
		return err
	}

	// 未啟用 graceful restart 時 server 不處理 SIGUSR2，訊號的預設動作會直接結束行程
	force, _ := cmd.Flags().GetBool("force")
	if err := checkGracefulRestart(pid, force); err != nil {
		return err
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
			return 0, fmt.Errorf("%s not found in the current directory: run this from the working directory of the running app (a compiled binary, not hyp run / go run)", path)
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in %s: %q", path, strings.TrimSpace(string(data)))
	}
	return pid, nil
}

// checkPIDWorksHere 確認 PID 檔記的行程是在這個目錄執行的（Linux，讀 /proc/<pid>/cwd）。
// 應用程式被 kill -9 或機器重開後 PID 檔會留下，該 PID 之後可能屬於別的行程；
// 先前只確認「有這個行程」就送 SIGUSR2，把無關的行程結束掉。
// 沒有 /proc 的系統（macOS、BSD）或讀不到時無從判斷，照舊放行
func checkPIDWorksHere(pid int) error {
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return nil
	}
	here, err := os.Getwd()
	if err != nil || sameDir(cwd, here) {
		return nil
	}
	return fmt.Errorf("PID %d from %s is not this app: its working directory is %s (stale PID file reused by another process); remove %s and start the app", pid, restartPIDFile, cwd, restartPIDFile)
}

func isProcessRunning(pid int) bool {
	// 送訊號 0 檢查行程是否存在
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}
