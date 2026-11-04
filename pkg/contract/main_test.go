package contract

import (
	"fmt"
	"os"
	"testing"
)

// TestMain 讓測試在暫存目錄執行：ObserveAll / RecordHistory 預設寫到模組根目錄的 .hyp，
// 不切換的話每跑一次測試就在這個 repo 多留下報告
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "contract-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.Chdir(os.TempDir()) // Windows 不能刪除工作目錄
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
