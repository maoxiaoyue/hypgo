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

// max_body_size：預設 10MB，必須大於 0（沒有不限的設定）
func TestMaxBodySize(t *testing.T) {
	cfg, warn, err := loadYAML(t, "server:\n  addr: \":8080\"\n")
	if err != nil || warn != "" || cfg.Server.MaxBodyBytes() != 10<<20 {
		t.Errorf("default = %d, warn = %q, err = %v", cfg.Server.MaxBodyBytes(), warn, err)
	}
	cfg, _, err = loadYAML(t, "server:\n  max_body_size: 512KB\n")
	if err != nil || cfg.Server.MaxBodyBytes() != 512<<10 {
		t.Errorf("512KB = %d, err = %v", cfg.Server.MaxBodyBytes(), err)
	}
	for _, bad := range []string{"0", "0MB", "-1", "lots", `""`} {
		if _, _, err := loadYAML(t, "server:\n  max_body_size: "+bad+"\n"); err == nil || !strings.Contains(err.Error(), "max_body_size") {
			t.Errorf("max_body_size %s accepted (err = %v)", bad, err)
		}
	}
}

// server.cors：沒寫是 nil（預設任何來源），allow_origins: [] 是空清單（不掛預設 CORS），兩者要分得出來
func TestCORSConfigKeys(t *testing.T) {
	cfg, warn, err := loadYAML(t, "server:\n  addr: \":8080\"\n")
	if err != nil || warn != "" || cfg.Server.CORS.AllowOrigins != nil {
		t.Errorf("absent: origins = %#v, warn = %q, err = %v", cfg.Server.CORS.AllowOrigins, warn, err)
	}
	cfg, warn, err = loadYAML(t, "server:\n  cors:\n    allow_origins: []\n")
	if err != nil || warn != "" || cfg.Server.CORS.AllowOrigins == nil || len(cfg.Server.CORS.AllowOrigins) != 0 {
		t.Errorf("empty: origins = %#v, warn = %q, err = %v", cfg.Server.CORS.AllowOrigins, warn, err)
	}
	cfg, warn, err = loadYAML(t, "server:\n  cors:\n    allow_origins: [\"https://app.example.com\"]\n    allow_credentials: true\n    max_age: 600\n")
	if c := cfg.Server.CORS; err != nil || warn != "" || len(c.AllowOrigins) != 1 || !c.AllowCredentials || c.MaxAge != 600 {
		t.Errorf("cors = %+v, warn = %q, err = %v", c, warn, err)
	}
	if _, warn, _ := loadYAML(t, "server:\n  cors:\n    allow_origin: [\"*\"]\n"); !strings.Contains(warn, "allow_origins") {
		t.Errorf("typo not reported: %q", warn)
	}
}

// database 的 hidb 行為設定：沒寫是 0（hidb 用預設值），明確寫 0 存成 -1（關閉）；Redis 的進階設定讀得進來
func TestDatabaseOptionKeys(t *testing.T) {
	cfg, warn, err := loadYAML(t, "database:\n  driver: postgres\n  dsn: x\n")
	if o := cfg.Database.GetDatabaseOptions(); err != nil || warn != "" || o != (DatabaseOptions{}) {
		t.Errorf("absent: %+v, warn = %q, err = %v", o, warn, err)
	}
	cfg, warn, err = loadYAML(t, "database:\n  driver: postgres\n  dsn: x\n  slow_query: 0\n"+
		"  conn_max_lifetime: 600\n  conn_max_idle_time: 0\n  strict_columns: true\n"+
		"  redis:\n    username: app\n    tls: true\n    pool_size: 20\n    dial_timeout: 2\n    read_timeout: 1\n    write_timeout: 1\n")
	if err != nil || warn != "" {
		t.Fatalf("warn = %q, err = %v", warn, err)
	}
	want := DatabaseOptions{SlowQuery: -1, ConnMaxLifetime: 600, ConnMaxIdleTime: -1, StrictColumns: true}
	if o := cfg.Database.GetDatabaseOptions(); o != want {
		t.Errorf("options = %+v, want %+v", o, want)
	}
	if r := cfg.Database.Redis.GetRedisOptions(); r != (RedisOptions{Username: "app", TLS: true, PoolSize: 20, DialTimeout: 2, ReadTimeout: 1, WriteTimeout: 1}) {
		t.Errorf("redis options = %+v", r)
	}
	if _, warn, _ := loadYAML(t, "database:\n  slow_querys: 5\n"); !strings.Contains(warn, "slow_query") {
		t.Errorf("typo not reported: %q", warn)
	}
}
