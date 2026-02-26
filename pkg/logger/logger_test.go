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
