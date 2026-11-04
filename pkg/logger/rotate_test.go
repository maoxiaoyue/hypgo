package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	// 底層以 MB 為單位，1MB 為最小門檻
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	chunk := strings.Repeat("x", 600<<10) + "\n"
	if _, err := r.Write([]byte(chunk)); err != nil { // 600KB，未達標
		t.Fatal(err)
	}
	if _, err := r.Write([]byte(chunk)); err != nil { // 1.2MB > 1MB → 應先輪轉再寫
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

// TestRotationConfigMapping RotationConfig 換算成 lumberjack 設定：
// max_size 進位到 MB（最少 1MB），max_age 為保留天數（無條件進位、最少 1 天）
func TestRotationConfigMapping(t *testing.T) {
	tests := []struct {
		size, age   string
		wantMB      int
		wantDays    int
		wantErrPart string
	}{
		{size: "100MB", age: "7d", wantMB: 100, wantDays: 7},
		{size: "100M", age: "1w", wantMB: 100, wantDays: 7},
		{size: "1.5GB", age: "12h", wantMB: 1536, wantDays: 1},
		{size: "512KB", age: "50ms", wantMB: 1, wantDays: 1},
		{size: "1048577", age: "36h", wantMB: 2, wantDays: 2},
		{size: "", age: "", wantMB: noSizeLimitMB, wantDays: 0},
		{size: "0", age: "0", wantMB: noSizeLimitMB, wantDays: 0},
		{size: "-5MB", wantErrPart: "max size"},
		{size: "abc", wantErrPart: "max size"},
		{size: "1e3", wantErrPart: "max size"},
		{size: "10MB", age: "-1d", wantErrPart: "max age"},
		{size: "10MB", age: "soon", wantErrPart: "max age"},
	}
	for _, tt := range tests {
		r, err := NewRotation(filepath.Join(t.TempDir(), "app.log"), &RotationConfig{MaxSize: tt.size, MaxAge: tt.age})
		if tt.wantErrPart != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Errorf("size=%q age=%q: err = %v, want %q error", tt.size, tt.age, err, tt.wantErrPart)
			}
			continue
		}
		if err != nil {
			t.Errorf("size=%q age=%q: unexpected error %v", tt.size, tt.age, err)
			continue
		}
		if r.lj.MaxSize != tt.wantMB || r.lj.MaxAge != tt.wantDays {
			t.Errorf("size=%q age=%q: got %dMB / %d days, want %dMB / %d days",
				tt.size, tt.age, r.lj.MaxSize, r.lj.MaxAge, tt.wantMB, tt.wantDays)
		}
		r.Close()
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
