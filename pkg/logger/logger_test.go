package logger

import (
	"bytes"
	"strings"
	"testing"
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
	l := NewLogger()
	// Disable colorizer for simpler testing
	l.colorize = false

	msg := l.formatMessage(INFO, "test message")
	if !strings.Contains(msg, "[INFO]") || !strings.Contains(msg, "test message") {
		t.Errorf("unexpected formatted message: %s", msg)
	}

	msgWithKV := l.formatMessage(DEBUG, "test with kv", "key", "value")
	if !strings.Contains(msgWithKV, "[DEBUG]") || !strings.Contains(msgWithKV, "key=value") {
		t.Errorf("unexpected formatted message with KV: %s", msgWithKV)
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
	var buf bytes.Buffer
	l, _ := New("info", "stdout", &buf, true)

	// BUG-3: 以前 colorize 永遠為 false
	l.Info("colored")

	output := buf.String()
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected color escape codes when colorize=true, got: %s", output)
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

// TestFormatMessagePrintfStyle Bug5 修復驗證：%s/%v/%d 格式動詞必須展開
func TestFormatMessagePrintfStyle(t *testing.T) {
	l, _ := New("debug", "stdout", &bytes.Buffer{}, false)

	// %s 展開
	msg := l.formatMessage(INFO, "hello %s", "world")
	if !strings.Contains(msg, "hello world") {
		t.Errorf("expected 'hello world', got: %s", msg)
	}

	// %v 展開
	msg = l.formatMessage(ERROR, "error: %v", "something failed")
	if !strings.Contains(msg, "error: something failed") {
		t.Errorf("expected 'error: something failed', got: %s", msg)
	}

	// %d 展開
	msg = l.formatMessage(INFO, "port: %d", 8080)
	if !strings.Contains(msg, "port: 8080") {
		t.Errorf("expected 'port: 8080', got: %s", msg)
	}

	// 多參數
	msg = l.formatMessage(INFO, "server %s on :%d", "started", 9090)
	if !strings.Contains(msg, "server started on :9090") {
		t.Errorf("expected 'server started on :9090', got: %s", msg)
	}

	// 不含 % 的結構化 KV 模式仍應正常運作
	msg = l.formatMessage(INFO, "request completed", "status", 200, "latency", "5ms")
	if !strings.Contains(msg, "status=200") || !strings.Contains(msg, "latency=5ms") {
		t.Errorf("expected KV pairs, got: %s", msg)
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
	l, _ := New("debug", "stdout", &bytes.Buffer{}, false)

	// BUG-8: 奇數鍵值對 — 最後一個 key 應顯示 (MISSING)
	msg := l.formatMessage(INFO, "test", "key1", "val1", "orphanKey")

	if !strings.Contains(msg, "key1=val1") {
		t.Errorf("expected key1=val1 in output, got: %s", msg)
	}
	if !strings.Contains(msg, "orphanKey=(MISSING)") {
		t.Errorf("expected orphanKey=(MISSING) for odd key, got: %s", msg)
	}
}
