//go:build !unix && !windows

package contract

import "time"

// processCPUTime 在無行程 CPU 時間 API 的平台（js / wasip1 / plan9）恆回 0，CPU 量測欄位顯示為 0
func processCPUTime() time.Duration { return 0 }
