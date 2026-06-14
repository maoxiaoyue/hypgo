//go:build !windows

// @chris
package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the HypGo application gracefully",
	Long:  `Restart the application with zero downtime using graceful shutdown`,
	RunE:  runRestart,
}

func runRestart(cmd *cobra.Command, args []string) error {
	// 查找正在運行的 HypGo 進程
	pid, err := findHypGoProcess()
	if err != nil {
		return fmt.Errorf("failed to find HypGo process: %w", err)
	}

	if pid == 0 {
		fmt.Println("❌ No HypGo process found running")
		fmt.Println("💡 Use 'hyp run' to start the application")
		return nil
	}

	fmt.Printf("🔄 Found HypGo process with PID: %d\n", pid)

	// 發送 SIGUSR2 信號進行熱重啟
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	fmt.Println("📤 Sending graceful restart signal...")
	if err := process.Signal(syscall.SIGUSR2); err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	// 等待新進程啟動
	fmt.Println("⏳ Waiting for new process to start...")
	time.Sleep(2 * time.Second)

	// 檢查新進程是否啟動成功
	newPid, err := findHypGoProcess()
	if err != nil {
		return fmt.Errorf("failed to check new process: %w", err)
	}

	if newPid != 0 && newPid != pid {
		fmt.Printf("✅ Application restarted successfully! New PID: %d\n", newPid)
	} else {
		fmt.Println("⚠️  Application may not have restarted properly")
	}

	return nil
}

func findHypGoProcess() (int, error) {
	// 查找 PID 文件
	pidFile := "hypgo.pid"
	if data, err := ioutil.ReadFile(pidFile); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && isProcessRunning(pid) {
			return pid, nil
		}
	}

	// 如果 PID 文件不存在或無效，嘗試通過進程列表查找
	output, err := exec.Command("pgrep", "-f", "main.go").Output()
	if err != nil {
		return 0, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if pid, err := strconv.Atoi(line); err == nil {
			return pid, nil
		}
	}

	return 0, nil
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// 發送信號 0 來檢查進程是否存在
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
