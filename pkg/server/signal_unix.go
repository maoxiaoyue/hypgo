//go:build !windows

package server

import (
	"os"
	"syscall"
)

// restartSignals 定義了在 Unix 系統上觸發優雅重啟的信號
var restartSignals = []os.Signal{syscall.SIGUSR2}
