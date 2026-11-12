//go:build !windows

package main

import "os/exec"

// bindChildLifetime 在 Windows 以 Job Object 讓應用程式隨 hyp run 一起結束（見 run_windows.go）。
// Unix 沒有可攜的等價機制（Linux 的 Pdeathsig 綁的是建立子行程的執行緒，macOS 沒有），維持原狀：
// hyp run 正常結束（Ctrl+C / SIGTERM）時仍會先停掉應用程式
func bindChildLifetime(cmd *exec.Cmd) {}
