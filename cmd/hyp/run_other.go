//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// bindChildLifetime 在 Windows 以 Job Object 讓應用程式隨 hyp run 一起結束（見 run_windows.go）。
// Unix 沒有可攜的等價機制（Linux 的 Pdeathsig 綁的是建立子行程的執行緒，macOS 沒有），維持原狀：
// hyp run 正常結束（Ctrl+C / SIGTERM）時仍會先停掉應用程式
func bindChildLifetime(cmd *exec.Cmd) {}

// prepareChild 啟動前的平台設定（Windows 才需要，見 run_windows.go）
func prepareChild(cmd *exec.Cmd) {}

// interruptChild 請應用程式優雅結束。
// hyp run 自己是被 Ctrl+C 結束時不送：終端機已把 SIGINT 送給整個前景行程群組，應用程式
// 正在優雅關閉；再補一個 SIGTERM，server 會當成第二個終止訊號而強制中斷連線。
// （只對 hyp run 這一個行程 kill -INT 時應用程式收不到訊號，stop 會等到逾時才 Kill；請用 kill 預設的 SIGTERM）
func interruptChild(cmd *exec.Cmd) error {
	if ttyInterrupt.Load() {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
