//go:build windows

package main

import "golang.org/x/sys/windows"

// canInterruptChild Ctrl+Break 只能經由主控台送出；沒有主控台的環境（部分 CI）會退回 Kill
func canInterruptChild() bool {
	_, err := windows.GetConsoleCP()
	return err == nil
}
