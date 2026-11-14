package config

import (
	"strings"
	"testing"
)

// trusted_proxies 有一筆無效即載入失敗。
// 歷史 bug：載入成功，啟動時只有一行警告、整份清單作廢
func TestTrustedProxiesAreValidated(t *testing.T) {
	_, _, err := loadYAML(t, "server:\n  trusted_proxies: [\"127.0.0.1\", \"10.0.0.0/33\"]\n")
	if err == nil || !strings.Contains(err.Error(), "10.0.0.0/33") {
		t.Errorf("err = %v, want it to name the bad entry", err)
	}
	if _, _, err := loadYAML(t, "server:\n  trusted_proxies: [\"127.0.0.1\", \"10.0.0.0/8\", \"::1\"]\n"); err != nil {
		t.Errorf("valid list rejected: %v", err)
	}
}

// shutdown_timeout：沒寫是 30，明確寫 0 保留（不等待），負數當 0；alt_svc_port 要在埠號範圍內
func TestShutdownTimeoutAndAltSvcPort(t *testing.T) {
	cfg, warn, err := loadYAML(t, "server:\n  addr: \":8080\"\n")
	if err != nil || warn != "" || cfg.Server.ShutdownTimeout != 30 {
		t.Errorf("default: timeout = %d, warn = %q, err = %v", cfg.Server.ShutdownTimeout, warn, err)
	}
	cfg, warn, err = loadYAML(t, "server:\n  shutdown_timeout: -5\n  alt_svc_port: 443\n")
	if err != nil || warn != "" {
		t.Fatalf("warn = %q, err = %v", warn, err)
	}
	if cfg.Server.ShutdownTimeout != 0 || !cfg.IsExplicit("server.shutdown_timeout") || cfg.Server.AltSvcPort != 443 {
		t.Errorf("timeout = %d, explicit = %v, port = %d", cfg.Server.ShutdownTimeout, cfg.IsExplicit("server.shutdown_timeout"), cfg.Server.AltSvcPort)
	}
	if _, _, err := loadYAML(t, "server:\n  alt_svc_port: 70000\n"); err == nil {
		t.Error("alt_svc_port 70000 accepted")
	}
}

// h1 / h2 / h3 是 pkg/server 接受的簡寫，設定檔也要收（先前被驗證擋下）
func TestProtocolShortNames(t *testing.T) {
	cfg, _, err := loadYAML(t, "server:\n  protocol: H2\n")
	if err != nil || cfg.Server.Protocol != "http2" {
		t.Errorf("protocol = %q, err = %v", cfg.Server.Protocol, err)
	}
}
