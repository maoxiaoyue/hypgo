package logger

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// 與輪轉無關的寫入錯誤（磁碟滿、EIO、配額）不進入「輪轉失敗」模式。
// 歷史 bug：任何寫入錯誤都當成輪轉失敗，之後每秒強制輪轉一次——每次切出一個新備份、
// 把 max_backups 內真正的歷史備份擠掉，stderr 交替印出失敗與恢復
func TestWriteErrorIsNotARotationFailure(t *testing.T) {
	stderr := captureErrOut(t)
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	rotations := 0
	realRotate, realWrite := ljRotate, ljWrite
	ljRotate = func(l *lumberjack.Logger) error { rotations++; return realRotate(l) }
	diskFull := errors.New("write app.log: no space left on device")
	failing := true
	ljWrite = func(l *lumberjack.Logger, p []byte) (int, error) {
		if failing {
			return 0, diskFull
		}
		return realWrite(l, p)
	}
	defer func(d time.Duration) { ljRotate, ljWrite, rotateRetry = realRotate, realWrite, d }(rotateRetry)
	rotateRetry = 0 // 每一筆都可以重試：舊行為會每筆輪轉一次

	// 既有的歷史備份
	for _, name := range []string{"app-2026-01-01T00-00-00.000.log", "app-2026-01-02T00-00-00.000.log"} {
		os.WriteFile(filepath.Join(dir, name), []byte("history\n"), 0o644)
	}
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB", MaxBackups: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < 5; i++ {
		if _, err := r.Write([]byte("line\n")); !errors.Is(err, diskFull) {
			t.Fatalf("write %d: err = %v, want the write error", i, err)
		}
	}
	if rotations != 0 {
		t.Errorf("%d forced rotation(s) for a plain write error", rotations)
	}
	if out := stderr.String(); strings.Contains(out, "cannot rotate") || strings.Contains(out, "rotated after") {
		t.Errorf("a write error was reported as a rotation failure: %q", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 3 {
		t.Errorf("%d files in the log directory: backups were created for nothing", len(entries))
	}
	for _, name := range []string{"app-2026-01-01T00-00-00.000.log", "app-2026-01-02T00-00-00.000.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("history backup %s is gone", name)
		}
	}

	// 寫得進去之後照常
	failing = false
	if _, err := r.Write([]byte("line\n")); err != nil {
		t.Errorf("write after the error cleared: %v", err)
	}
}

// 輪轉失敗期間檔案回到上限以下（被別的行程輪轉或清掉）時不再強制輪轉
func TestBlockedRotationStopsWhenFileIsSmallAgain(t *testing.T) {
	stderr := captureErrOut(t)
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	rotations := 0
	realRotate := ljRotate
	ljRotate = func(l *lumberjack.Logger) error {
		rotations++
		l.Close()
		return errors.New("rename blocked")
	}
	defer func(d time.Duration) { ljRotate, rotateRetry = realRotate, d }(rotateRetry)
	rotateRetry = 0

	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for i := 0; i < 1100; i++ { // 超過 1MB：輪轉失敗，改為直接附加
		r.Write(line)
	}
	if !strings.Contains(stderr.String(), "cannot rotate") {
		t.Fatalf("setup: rotation did not fail: %q", stderr.String())
	}
	// 別人把檔案清掉了
	if err := os.Truncate(logfile, 0); err != nil {
		t.Fatal(err)
	}
	before := rotations
	for i := 0; i < 3; i++ {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if rotations != before {
		t.Errorf("%d more rotation attempt(s) although the file is below max_size", rotations-before)
	}
	if !strings.Contains(stderr.String(), "below max_size again") {
		t.Errorf("no note about leaving the failed state: %q", stderr.String())
	}
}
