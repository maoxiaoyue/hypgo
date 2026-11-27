package config

import (
	"strings"
	"testing"
)

// 空值等於沒寫。歷史 bug：`read_timeout:` 讓鍵算成明確設定、值是 0，逾時與 max_handlers 被關掉；
// `max_body_size:` 載入失敗
func TestNullValuesAreNotExplicit(t *testing.T) {
	cfg, _, err := loadYAML(t, "server:\n  read_timeout:\n  write_timeout: ~\n  idle_timeout: null\n  max_handlers:\n  max_body_size:\nlogger:\n  rotation:\n    max_backups:\n")
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Server
	if s.ReadTimeout != 30 || s.WriteTimeout != 30 || s.IdleTimeout != 120 || s.MaxHandlers != 1000 {
		t.Errorf("null values turned settings off: read=%d write=%d idle=%d max_handlers=%d", s.ReadTimeout, s.WriteTimeout, s.IdleTimeout, s.MaxHandlers)
	}
	if s.MaxBodySize != DefaultMaxBodySize || cfg.Logger.Rotation.MaxBackups != 10 {
		t.Errorf("max_body_size=%q max_backups=%d", s.MaxBodySize, cfg.Logger.Rotation.MaxBackups)
	}
	// 明確的 0 仍然代表關閉
	cfg, _, err = loadYAML(t, "server:\n  read_timeout: 0\n")
	if err != nil || cfg.Server.ReadTimeout != 0 {
		t.Errorf("explicit 0: read=%d err=%v", cfg.Server.ReadTimeout, err)
	}
}

// 合併鍵與 alias。歷史 bug：`<<` 被報成未知的鍵（擋下 hyp lint），帶進來的 0 不算明確設定，
// `server: *alias` 整段不檢查
func TestMergeKeysAndAliases(t *testing.T) {
	cfg, out, err := loadYAML(t, "defaults: &d\n  read_timeout: 0\n  write_timeout: 5\nserver:\n  <<: *d\n  addr: \":9000\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("merge key reported: %s", out)
	}
	if cfg.Server.ReadTimeout != 0 || cfg.Server.WriteTimeout != 5 || cfg.Server.Addr != ":9000" {
		t.Errorf("read=%d write=%d addr=%q", cfg.Server.ReadTimeout, cfg.Server.WriteTimeout, cfg.Server.Addr)
	}
	// 合併進來的未知鍵、alias 指向的區段都照常檢查
	_, out, _ = loadYAML(t, "defaults: &d\n  adress: x\nserver:\n  <<: [*d]\n")
	if !strings.Contains(out, "server.adress") {
		t.Errorf("unknown key brought in by a merge is not reported: %q", out)
	}
	_, out, _ = loadYAML(t, "base: &b\n  adress: x\nserver: *b\n")
	if !strings.Contains(out, "server.adress") {
		t.Errorf("aliased section is not checked: %q", out)
	}
}

// 頂層的 mapping 是使用者自己的區段。歷史 bug：redis: / db: / tls: 與巢狀鍵同名就被報「少了縮排」
func TestTopLevelSectionsAreNotIndentationMistakes(t *testing.T) {
	_, out, err := loadYAML(t, "server:\n  addr: \":9000\"\nredis:\n  addr: x\ndb:\n  host: y\ntls:\n  cert: z\n")
	if err != nil || out != "" {
		t.Errorf("custom sections reported: %q (err %v)", out, err)
	}
	// 像錯字的頂層鍵、寫到最外層的純量仍然提示
	for yaml, want := range map[string]string{
		"loggers:\n  level: debug\n": `did you mean "logger"`,
		"server:\naddr: \":9000\"\n": "indentation",
	} {
		if _, out, _ := loadYAML(t, yaml); !strings.Contains(out, want) {
			t.Errorf("%q: warning = %q, want %q", yaml, out, want)
		}
	}
}

// 空的環境變數視同沒設定。歷史 bug：HYPGO_SERVER_CORS_ALLOW_ORIGINS="" 把 allow_origins: []（關閉 CORS）
// 換成 nil（掛回預設的 *）、HYPGO_DATABASE_DSN="" 清掉 DSN、整數欄位載入失敗
func TestEmptyEnvIsUnset(t *testing.T) {
	t.Setenv("HYPGO_SERVER_CORS_ALLOW_ORIGINS", "")
	t.Setenv("HYPGO_DATABASE_DSN", "")
	t.Setenv("HYPGO_SERVER_READ_TIMEOUT", "")
	cfg, _, err := loadYAML(t, "server:\n  read_timeout: 7\n  cors:\n    allow_origins: []\ndatabase:\n  driver: postgres\n  dsn: postgres://u@h/db\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.CORS.AllowOrigins == nil || len(cfg.Server.CORS.AllowOrigins) != 0 {
		t.Errorf("allow_origins = %#v, want an empty non-nil list", cfg.Server.CORS.AllowOrigins)
	}
	if cfg.Database.DSN != "postgres://u@h/db" || cfg.Server.ReadTimeout != 7 {
		t.Errorf("dsn=%q read=%d", cfg.Database.DSN, cfg.Server.ReadTimeout)
	}
}

// yaml:",inline" 嵌入 Config。歷史 bug：server / logger 全被報成未知的鍵，嚴格模式拒絕載入
func TestInlineEmbeddedConfig(t *testing.T) {
	type app struct {
		Config  `yaml:",inline"`
		Feature struct {
			On bool `yaml:"on"`
		} `yaml:"feature"`
	}
	warnings, explicit := inspectYAML("c.yaml", []byte("server:\n  addr: \":1\"\n  adress: x\nlogger:\n  level: debug\nfeature:\n  on: true\n"), &app{}, true)
	if len(warnings) != 1 || warnings[0].Key != "server.adress" {
		t.Errorf("warnings = %v", warnings)
	}
	if !explicit["server.addr"] || !explicit["feature.on"] {
		t.Errorf("explicit = %v", explicit)
	}
}
