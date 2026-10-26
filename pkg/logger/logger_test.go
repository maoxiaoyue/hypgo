package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGetLevelString(t *testing.T) {
	l := NewLogger()

	tests := []struct {
		level    Level
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{NOTICE, "NOTICE"},
		{WARNING, "WARNING"},
		{ERROR, "ERROR"},
		{EMERGENCY, "EMERGENCY"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		result := l.getLevelString(tt.level)
		if result != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, result)
		}
	}
}

func TestFormatMessage(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "", &buf, false)

	l.Info("test message")
	if msg := buf.String(); !strings.Contains(msg, "[INFO]") || !strings.Contains(msg, "test message") {
		t.Errorf("unexpected formatted message: %s", msg)
	}

	buf.Reset()
	l.Debug("test with kv", "key", "value")
	if msg := buf.String(); !strings.Contains(msg, "[DEBUG]") || !strings.Contains(msg, "| key=value") {
		t.Errorf("unexpected formatted message with KV: %s", msg)
	}
}

func TestLoggerOutput(t *testing.T) {
	l := NewLogger()
	l.SetLevel(INFO)

	var buf bytes.Buffer
	l.SetOutput(&buf)

	// DEBUG should be ignored
	l.Debug("debug message")
	if buf.Len() > 0 {
		t.Errorf("expected debug message to be ignored, got %s", buf.String())
	}

	// INFO should be written
	buf.Reset()
	l.Info("info message")
	if !strings.Contains(buf.String(), "info message") {
		t.Errorf("expected info message to be logged")
	}

	// WARNING should be written
	buf.Reset()
	l.Warning("warn message")
	if !strings.Contains(buf.String(), "warn message") {
		t.Errorf("expected warn message to be logged")
	}
}

func TestGetLogger(t *testing.T) {
	l1 := GetLogger()
	l2 := GetLogger()

	if l1 != l2 {
		t.Errorf("GetLogger should return a singleton instance")
	}
}

// ===== BUG 修復驗證測試 =====

func TestNewDoesNotPanic(t *testing.T) {
	var buf bytes.Buffer
	l, err := New("info", "stdout", &buf, false)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	// BUG-1: 以前會 panic: nil pointer dereference
	l.Info("hello")

	output := buf.String()
	if !strings.Contains(output, "INFO") {
		t.Errorf("expected output to contain INFO, got: %s", output)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain hello, got: %s", output)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("error", "stdout", &buf, false)

	// BUG-2: 以前 level 未設定，所有等級都會輸出
	l.Info("should not appear")
	l.Debug("should not appear")
	l.Error("should appear")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Errorf("INFO/DEBUG should be filtered when level=error, got: %s", output)
	}
	if !strings.Contains(output, "should appear") {
		t.Errorf("ERROR should appear when level=error, got: %s", output)
	}
}

func TestNewRespectsColorize(t *testing.T) {
	// 色碼只在 colors=true 且目的地為終端機時輸出；以替換 isTerminal 模擬終端機
	orig := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	defer func() { isTerminal = orig }()

	var buf bytes.Buffer
	l, _ := New("info", "stdout", &buf, true)

	// BUG-3: 以前 colorize 永遠為 false
	l.Info("colored")

	output := buf.String()
	if !strings.Contains(output, ColorGreen+"[INFO]"+ColorReset) {
		t.Errorf("expected color escape codes when colorize=true, got: %q", output)
	}

	buf.Reset()
	off, _ := New("info", "stdout", &buf, false)
	off.Info("plain")
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("colors=false must not emit escape codes, got: %q", buf.String())
	}
}

func TestSetOutputAfterNew(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("info", "stdout", &buf, false)

	// BUG-5: 以前會 panic
	var newBuf bytes.Buffer
	l.SetOutput(&newBuf)

	l.Info("after set output")
	output := newBuf.String()
	if !strings.Contains(output, "after set output") {
		t.Errorf("expected output in new buffer, got: %s", output)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", DEBUG},
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"INFO", INFO},
		{"warning", WARNING},
		{"warn", WARNING},
		{"error", ERROR},
		{"emergency", EMERGENCY},
		{"unknown", INFO},
		{"", INFO},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.expected {
			t.Errorf("parseLevel(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// TestPrintfMethods 驗證 printf-style 方法（Infof/Errorf/...）會展開格式動詞
func TestPrintfMethods(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "stdout", &buf, false)

	// %s 展開
	buf.Reset()
	l.Infof("hello %s", "world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("expected 'hello world', got: %s", buf.String())
	}

	// %v 展開
	buf.Reset()
	l.Errorf("error: %v", "something failed")
	if !strings.Contains(buf.String(), "error: something failed") {
		t.Errorf("expected 'error: something failed', got: %s", buf.String())
	}

	// 多參數
	buf.Reset()
	l.Infof("server %s on :%d", "started", 9090)
	if !strings.Contains(buf.String(), "server started on :9090") {
		t.Errorf("expected 'server started on :9090', got: %s", buf.String())
	}
}

// TestInfoDoesNotInterpretPercent 驗證拆分後的正確性修復：
// KV 模式（Info）不再把訊息中字面的 % 當成格式動詞（修正啟發式偵測地雷）
func TestInfoDoesNotInterpretPercent(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "stdout", &buf, false)

	// 含字面百分比的訊息 + KV：% 必須原樣輸出，不可被吃掉或變亂碼
	l.Info("upload progress 50% done", "file", "video.mp4")
	out := buf.String()
	if !strings.Contains(out, "upload progress 50% done") {
		t.Errorf("literal %% must be preserved in KV mode, got: %s", out)
	}
	if !strings.Contains(out, "file=video.mp4") {
		t.Errorf("expected KV pair file=video.mp4, got: %s", out)
	}
	if strings.Contains(out, "%!") || strings.Contains(out, "(MISSING)") {
		t.Errorf("KV mode must not run Sprintf on the message, got: %s", out)
	}

	// 結構化 KV 模式正常運作
	buf.Reset()
	l.Info("request completed", "status", 200, "latency", "5ms")
	out = buf.String()
	if !strings.Contains(out, "status=200") || !strings.Contains(out, "latency=5ms") {
		t.Errorf("expected KV pairs, got: %s", out)
	}
}

// TestJSONFormat 驗證 slog JSON 後端輸出標準結構化日誌
func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "stdout", &buf, false)
	l.SetFormat("json")

	l.Info("user login", "user_id", 42, "ip", "1.2.3.4")
	out := buf.String()

	// slog JSONHandler 應輸出標準欄位
	for _, want := range []string{`"level"`, `"msg":"user login"`, `"user_id":42`, `"ip":"1.2.3.4"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected JSON output to contain %s, got: %s", want, out)
		}
	}
}

// TestNilLoggerSafety Bug6 修復驗證：nil *Logger 不應 panic
func TestNilLoggerSafety(t *testing.T) {
	var l *Logger

	// 所有方法都不應 panic
	l.Debug("test")
	l.Info("test")
	l.Notice("test")
	l.Warn("test")
	l.Warning("test")
	l.Error("test")
	l.Emergency("test")
	l.SetLevel(DEBUG)
	l.SetOutput(nil)
	l.Close()
}

func TestFormatMessageOddKeysAndValues(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "stdout", &buf, false)

	// BUG-8: 奇數鍵值對 — 最後一個 key 應顯示 (MISSING)
	l.Info("test", "key1", "val1", "orphanKey")
	msg := buf.String()

	if !strings.Contains(msg, "key1=val1") {
		t.Errorf("expected key1=val1 in output, got: %s", msg)
	}
	if !strings.Contains(msg, "orphanKey=(MISSING)") {
		t.Errorf("expected orphanKey=(MISSING) for odd key, got: %s", msg)
	}
}

// TestConcurrentLoggingWithReconfigure 多個 goroutine 同時寫入、切換層級 / 格式並強制輪轉（以 -race 執行）
func TestConcurrentLoggingWithReconfigure(t *testing.T) {
	dir := t.TempDir()
	rot, err := NewRotation(filepath.Join(dir, "app.log"), &RotationConfig{MaxSize: "1MB", MaxBackups: 3})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := New("debug", "", rot, true)
	defer rot.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				l.Info("work", "worker", id, "j", j, "note", "a b")
				l.Warnf("worker %d step %d", id, j)
				l.Slog().Info("via slog", "id", id)
			}
		}(i)
	}
	wg.Add(3)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			l.SetLevel(Level(j % 6))
		}
	}()
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			if j%2 == 0 {
				l.SetFormat("json")
			} else {
				l.SetFormat("text")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for j := 0; j < 5; j++ {
			_ = rot.Rotate()
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
}

// TestSlogAccessor Slog() 共用 Logger 的輸出、層級與格式，支援 WithAttrs / WithGroup
func TestSlogAccessor(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("info", "", &buf, false)
	sl := l.Slog().With("svc", "api").WithGroup("req")

	sl.Debug("hidden")
	sl.Info("handled", "id", 7)
	if out := buf.String(); !strings.Contains(out, "[INFO] handled | svc=api req.id=7") || strings.Contains(out, "hidden") {
		t.Errorf("unexpected slog text output: %q", out)
	}

	buf.Reset()
	l.SetFormat("json")
	sl.Log(context.Background(), slogNotice, "n", "id", 8)
	if out := buf.String(); !strings.Contains(out, `"level":"NOTICE"`) || !strings.Contains(out, `"svc":"api","req":{"id":8}`) {
		t.Errorf("unexpected slog JSON output: %q", out)
	}

	var nilLogger *Logger
	nilLogger.Slog().Info("discarded") // 不得 panic
}

// TestFatalClosesBeforeExit Fatal / Fatalf 先寫出並關閉日誌檔再退出
func TestFatalClosesBeforeExit(t *testing.T) {
	var code int
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()

	logfile := filepath.Join(t.TempDir(), "app.log")
	l, err := NewWithRotation("info", logfile, &RotationConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	l.Fatalf("fatal %s", "boom")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	l.Info("after fatal") // 已關閉：丟棄
	data, _ := os.ReadFile(logfile)
	if !strings.Contains(string(data), "[EMERGENCY] fatal boom") || strings.Contains(string(data), "after fatal") {
		t.Errorf("unexpected log file content: %q", data)
	}
	// 關閉後檔案可被刪除（Windows 上開著的檔案無法刪除）
	if err := os.Remove(logfile); err != nil {
		t.Errorf("log file should be closed after Fatal: %v", err)
	}
}

// TestTextValueFormatting 文字模式各種值的呈現
func TestTextValueFormatting(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("info", "", &buf, false)
	l.Info("vals", "int", 42, "float", 1.5, "bool", true, "dur", 5*time.Millisecond,
		"err", errors.New("disk full"), "cn", "中文", "slice", []int{1, 2})
	want := `| int=42 float=1.5 bool=true dur=5ms err="disk full" cn=中文 slice="[1 2]"`
	if !strings.Contains(buf.String(), want) {
		t.Errorf("got %q, want suffix %q", buf.String(), want)
	}
}
