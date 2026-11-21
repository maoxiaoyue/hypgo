package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logger.format：預設 text，不分大小寫，其他值載入失敗；output 的 stdout / stderr 不分大小寫
func TestLoggerFormatAndOutputNormalised(t *testing.T) {
	cfg, _, err := loadYAML(t, "server:\n  addr: \":8080\"\n")
	if err != nil || cfg.Logger.Format != "text" {
		t.Fatalf("default format = %q, %v", cfg.Logger.Format, err)
	}
	cfg, _, err = loadYAML(t, "server:\n  addr: \":8080\"\nlogger:\n  format: \" JSON \"\n  output: \" STDOUT \"\n")
	if err != nil || cfg.Logger.Format != "json" || cfg.Logger.Output != "stdout" {
		t.Fatalf("format = %q, output = %q, %v", cfg.Logger.Format, cfg.Logger.Output, err)
	}
	cfg, _, err = loadYAML(t, "server:\n  addr: \":8080\"\nlogger:\n  output: \"Logs/App.log\"\n")
	if err != nil || cfg.Logger.Output != "Logs/App.log" {
		t.Errorf("a file path must keep its case: %q, %v", cfg.Logger.Output, err)
	}
	if _, _, err = loadYAML(t, "server:\n  addr: \":8080\"\nlogger:\n  format: xml\n"); err == nil || !strings.Contains(err.Error(), "logger.format") {
		t.Errorf("format xml: %v", err)
	}
	// 先前 max_age: 1M 被當成 1 分鐘、進位成保留 1 天
	if _, _, err = loadYAML(t, "server:\n  addr: \":8080\"\nlogger:\n  rotation:\n    max_age: 1M\n"); err == nil || !strings.Contains(err.Error(), "max_age") {
		t.Errorf("max_age 1M: %v", err)
	}

	t.Setenv("HYPGO_LOGGER_FORMAT", "json")
	if cfg, _, err = loadYAML(t, "server:\n  addr: \":8080\"\n"); err != nil || cfg.Logger.Format != "json" {
		t.Errorf("HYPGO_LOGGER_FORMAT: %q, %v", cfg.Logger.Format, err)
	}
}

// cfg.NewLogger 套用整個 logger 區段。先前模板以 logger.New 建立：rotation 一律是 100MB / 10 份
func TestNewLoggerAppliesConfig(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "logs", "app.log")
	cfg, _, err := loadYAML(t, "server:\n  addr: \":8080\"\nlogger:\n  level: Debug\n  format: json\n  output: \""+filepath.ToSlash(logfile)+"\"\n  rotation:\n    max_size: 1MB\n    max_backups: 1\n    max_age: 2d\n    compress: false\n")
	if err != nil {
		t.Fatal(err)
	}
	log, err := cfg.NewLogger()
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1000)
	for i := 0; i < 1500; i++ { // 約 1.6MB：設定生效的話會切出一份備份
		log.Debug(line)
	}
	log.Close()
	entries, _ := os.ReadDir(filepath.Dir(logfile))
	if len(entries) != 2 {
		t.Errorf("%d file(s) in logs/, want app.log + 1 backup (rotation.max_size not applied)", len(entries))
	}
	data, _ := os.ReadFile(logfile)
	if !bytes.HasPrefix(data, []byte(`{"time":`)) || !bytes.Contains(data, []byte(`"level":"DEBUG"`)) {
		t.Errorf("format / level not applied: %.100q", data)
	}
}
