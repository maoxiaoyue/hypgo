package logger

import (
	"io"
	"testing"
)

// BenchmarkTextKV 文字模式 KV 輸出（3 組 key/value）
func BenchmarkTextKV(b *testing.B) {
	l, _ := New("info", "", io.Discard, false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("request done", "status", 200, "path", "/api/users", "ms", 5)
	}
}

// BenchmarkTextPrintf 文字模式 printf 輸出
func BenchmarkTextPrintf(b *testing.B) {
	l, _ := New("info", "", io.Discard, false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Infof("server on %s (pid=%d)", ":8080", 1234)
	}
}

// BenchmarkJSONKV JSON 模式 KV 輸出
func BenchmarkJSONKV(b *testing.B) {
	l, _ := New("info", "", io.Discard, false)
	l.SetFormat("json")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("request done", "status", 200, "path", "/api/users", "ms", 5)
	}
}

// BenchmarkFiltered 低於門檻的日誌（應近乎零成本）
func BenchmarkFiltered(b *testing.B) {
	l, _ := New("warning", "", io.Discard, false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Debug("request done", "status", 200, "path", "/api/users", "ms", 5)
	}
}
