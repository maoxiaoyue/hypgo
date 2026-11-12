//go:build windows

package main

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	devJobOnce sync.Once
	devJob     windows.Handle
)

// bindChildLifetime 把應用程式行程放進設有 KILL_ON_JOB_CLOSE 的 Job Object：hyp run 結束時
// （含被工作管理員或 taskkill /F 強制結束）job handle 隨之關閉，系統會一併結束應用程式。
// 先前強制結束 hyp run 後，應用程式仍留在背景占著埠，下次啟動因埠被占用而失敗。
// 任何一步失敗都只是退回原本的行為，不影響啟動
func bindChildLifetime(cmd *exec.Cmd) {
	devJobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			return
		}
		devJob = job
	})
	if devJob == 0 || cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.AssignProcessToJobObject(devJob, h)
}
