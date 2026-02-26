//go:build windows

package server

import "os"

// restartSignals 定義了在 Windows 系統上觸發優雅重啟的信號
var restartSignals = []os.Signal{os.Interrupt}
