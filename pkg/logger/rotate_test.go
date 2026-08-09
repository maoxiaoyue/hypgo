package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backups 回傳目前的備份檔數量（app-*.log 型式）
func backups(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "app-*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestRotationWriteTriggersBySize 回歸測試：Write 必須依 max_size 自動輪轉。
// 歷史 bug：Rotation.Write 只寫入與累加 size，從未觸發 Rotate()，
// 而 Rotate() 又零 caller——輪轉機制完全失效、日誌無上限增長。
func TestRotationWriteTriggersBySize(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1KB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	chunk := strings.Repeat("x", 600) + "\n"
	if _, err := r.Write([]byte(chunk)); err != nil { // 601B，未達標
		t.Fatal(err)
	}
	if _, err := r.Write([]byte(chunk)); err != nil { // 601+601 > 1024 → 應先輪轉再寫
		t.Fatal(err)
	}

	if got := backups(t, dir); len(got) != 1 {
		t.Fatalf("expected 1 backup after size-triggered rotation, got %d: %v", len(got), got)
	}
	// 當前檔應只含第二筆
	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(chunk) {
		t.Errorf("current file size = %d, want %d (only the post-rotation write)", len(data), len(chunk))
	}
}

// TestRotationWriteTriggersByAge Write 依 max_age（自檔案開啟起算，非 ModTime）輪轉
func TestRotationWriteTriggersByAge(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	r, err := NewRotation(logfile, &RotationConfig{MaxAge: "50ms"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := r.Write([]byte("second\n")); err != nil { // 已超齡 → 應輪轉
		t.Fatal(err)
	}

	if got := backups(t, dir); len(got) != 1 {
		t.Fatalf("expected 1 backup after age-triggered rotation, got %d: %v", len(got), got)
	}
}

// TestRotationForceRotate Rotate() 為強制輪轉（無視門檻）
func TestRotationForceRotate(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	r, err := NewRotation(logfile, &RotationConfig{}) // 無任何門檻
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.Rotate(); err != nil {
		t.Fatal(err)
	}

	if got := backups(t, dir); len(got) != 1 {
		t.Fatalf("expected 1 backup after forced rotation, got %d: %v", len(got), got)
	}
}

// TestNewRotationCreatesDir 開檔時自動建立日誌目錄
func TestNewRotationCreatesDir(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "logs", "nested", "app.log")

	r, err := NewRotation(logfile, &RotationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Write([]byte("ok\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logfile); err != nil {
		t.Errorf("log file should exist: %v", err)
	}
}

// TestNewWithRotation Logger 檔案輸出接上輪轉 writer，Close 一併收尾
func TestNewWithRotation(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	l, err := NewWithRotation("info", logfile, &RotationConfig{MaxSize: "100MB"}, false)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello rotation", "k", "v")
	l.Close()

	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello rotation") {
		t.Errorf("log file should contain the message, got %q", string(data))
	}
}
