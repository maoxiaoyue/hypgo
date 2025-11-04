package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer

	log, err := New("debug", "stdout", &buf, false)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// 測試日誌級別
	log.Debug("debug message")
	log.Info("info message")
	log.Warning("warning message")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Error("Debug message not logged")
	}
}
