package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadYAML 把內容寫成暫存的 config.yaml 後載入；警告收進回傳的字串
func loadYAML(t *testing.T, content string) (*Config, string, error) {
	t.Helper()
	var buf bytes.Buffer
	old := WarningOutput
	WarningOutput = &buf
	t.Cleanup(func() { WarningOutput = old })

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	return cfg, buf.String(), err
}

// 打錯的鍵要有警告。
// 歷史 bug：tls 底下寫 enable（少一個 d）載入成功而 TLS 關閉；sever / adress、少縮排都靜默退回預設值
func TestUnknownKeysAreReported(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string // 警告應包含的片段；空 = 不該有警告
	}{
		{"tls.enable", "server:\n  tls:\n    enable: true\n", []string{"server.tls.enable", `did you mean "enabled"`, ":3:"}},
		{"sever", "sever:\n  addr: :9443\n", []string{"sever", `did you mean "server"`}},
		{"adress", "server:\n  adress: :9443\n", []string{"server.adress", "unknown key"}},
		{"wrong indentation", "server:\naddr: :9443\n", []string{"addr", "indentation"}},
		{"wrong indentation, unique key", "server:\nread_timeout: 5\n", []string{"did you mean server.read_timeout", "indentation"}},
		{"nested key at top level, ambiguous", "max_open_conns: 5\n", []string{"max_open_conns", "indentation"}},
		{"trusted_proxy", "server:\n  trusted_proxy: [10.0.0.0/8]\n", []string{"server.trusted_proxy", "trusted_proxies"}},
		{"unknown nested without suggestion", "logger:\n  flavour: spicy\n", []string{"logger.flavour", "unknown key"}},
		{"replica typo", "database:\n  replicas:\n    - dsn: x\n      max_open_conn: 5\n", []string{"database.replicas[0].max_open_conn", "max_open_conns"}},
		{"empty file", "", []string{"no settings"}},
		{"comments only", "# nothing here\n", []string{"no settings"}},
		// 使用者自己的頂層區段不算
		{"custom top-level section", "server:\n  addr: :9000\nmyapp:\n  feature: true\npayment:\n  key: x\nservice:\n  name: y\n", nil},
		{"all known", "server:\n  addr: :9000\n  tls:\n    enabled: false\nlogger:\n  rotation:\n    max_backups: 3\n", nil},
	}
	for _, c := range cases {
		cfg, out, err := loadYAML(t, c.yaml)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(c.want) == 0 {
			if out != "" || len(cfg.Warnings()) != 0 {
				t.Errorf("%s: unexpected warning: %s", c.name, out)
			}
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: warning should contain %q, got: %s", c.name, w, out)
			}
		}
		if len(cfg.Warnings()) == 0 {
			t.Errorf("%s: Warnings() is empty", c.name)
		}
	}
}

// 嚴格模式：有警告就載入失敗
func TestStrictModeFailsOnWarnings(t *testing.T) {
	t.Setenv(StrictEnv, "1")
	if _, _, err := loadYAML(t, "server:\n  tls:\n    enable: true\n"); err == nil || !strings.Contains(err.Error(), "server.tls.enable") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := loadYAML(t, "server:\n  addr: :9000\nmyapp:\n  x: 1\n"); err != nil {
		t.Errorf("a clean config should load in strict mode: %v", err)
	}
	t.Setenv(StrictEnv, "0")
	if _, _, err := loadYAML(t, "server:\n  tls:\n    enable: true\n"); err != nil {
		t.Errorf("%s=0 is not strict: %v", StrictEnv, err)
	}
}

// 明確給 0 或負數 = 關閉；沒給 = 預設值。
// 歷史 bug：0 一律被改回預設值（無法關閉），負的逾時原樣傳給 http.Server，等於沒有逾時卻沒有訊息
func TestExplicitZeroAndNegativeMeanOff(t *testing.T) {
	cfg, _, err := loadYAML(t, "server:\n  read_timeout: 0\n  write_timeout: -5\n  idle_timeout: -1\n  max_handlers: 0\n  max_concurrent_streams: -3\ndatabase:\n  max_idle_conns: -2\nlogger:\n  rotation:\n    max_backups: 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ReadTimeout != 0 || cfg.Server.WriteTimeout != 0 || cfg.Server.IdleTimeout != 0 || cfg.Server.MaxHandlers != 0 || cfg.Logger.Rotation.MaxBackups != 0 {
		t.Errorf("explicit zero / negative should mean off: %+v rotation=%+v", cfg.Server, cfg.Logger.Rotation)
	}
	// 沒有「關閉」語意的欄位：0 / 負數仍用預設值
	if cfg.Server.MaxConcurrentStreams != 250 || cfg.Database.MaxIdleConns != 10 {
		t.Errorf("streams=%d idle=%d, want defaults", cfg.Server.MaxConcurrentStreams, cfg.Database.MaxIdleConns)
	}

	cfg, _, _ = loadYAML(t, "server:\n  addr: :9000\n")
	if cfg.Server.ReadTimeout != 30 || cfg.Server.IdleTimeout != 120 || cfg.Server.MaxHandlers != 1000 || cfg.Logger.Rotation.MaxBackups != 10 {
		t.Errorf("unset keys should get defaults: %+v", cfg.Server)
	}

	// 直接建構的 Config：0 視為沒給；負數同樣當成 0
	direct := &Config{}
	direct.Server.WriteTimeout = -9
	direct.ApplyDefaults()
	if direct.Server.ReadTimeout != 30 || direct.Server.WriteTimeout != 30 {
		t.Errorf("a Config built in code keeps the old zero-means-default rule: %+v", direct.Server)
	}
}

func TestValidationAcceptsAndRejects(t *testing.T) {
	ok := map[string]string{
		"level error":       "logger:\n  level: error\n",
		"level WARN":        "logger:\n  level: WARN\n",
		"level Info":        "logger:\n  level: Info\n",
		"driver postgresql": "database:\n  driver: postgresql\n",
		"driver PG":         "database:\n  driver: PG\n",
		"driver MySQL":      "database:\n  driver: MySQL\n",
		"protocol HTTP2":    "server:\n  protocol: HTTP2\n",
		"addr host:port":    "server:\n  addr: 127.0.0.1:9000\n",
		"addr ipv6":         "server:\n  addr: \"[::1]:9000\"\n",
		"rotation":          "logger:\n  rotation:\n    max_size: 1.5GB\n    max_age: 2w\n",
		"rotation duration": "logger:\n  rotation:\n    max_age: 36h30m\n",
	}
	for name, y := range ok {
		if _, _, err := loadYAML(t, y); err != nil {
			t.Errorf("%s should be accepted: %v", name, err)
		}
	}
	bad := map[string][2]string{
		"level trace":      {"logger:\n  level: trace\n", "log level"},
		"driver sqlite":    {"database:\n  driver: sqlite\n", "driver"},
		"addr no colon":    {"server:\n  addr: \"8080\"\n", "server.addr"},
		"addr text":        {"server:\n  addr: not an address\n", "server.addr"},
		"addr bad port":    {"server:\n  addr: \":99999\"\n", "port"},
		"addr named port":  {"server:\n  addr: \":http\"\n", "port"},
		"rotation size":    {"logger:\n  rotation:\n    max_size: banana\n", "max_size"},
		"rotation age":     {"logger:\n  rotation:\n    max_age: soon\n", "max_age"},
		"replica no dsn":   {"database:\n  replicas:\n    - max_open_conns: 5\n", "replicas[0].dsn"},
		"http3 needs tls":  {"server:\n  protocol: http3\n", "TLS"},
		"tls needs certs":  {"server:\n  tls:\n    enabled: true\n", "cert_file"},
		"timeout not int":  {"server:\n  read_timeout: 30s\n", "unmarshal"},
		"duplicate key":    {"server:\n  addr: :1\n  addr: :2\n", "already defined"},
		"tls enabled text": {"server:\n  tls:\n    enabled: maybe\n", "unmarshal"},
	}
	for name, c := range bad {
		if _, _, err := loadYAML(t, c[0]); err == nil || !strings.Contains(err.Error(), c[1]) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c[1])
		}
	}
	// 正規化後的值
	cfg, _, _ := loadYAML(t, "logger:\n  level: WARN\ndatabase:\n  driver: PostgreSQL\nserver:\n  protocol: AUTO\n")
	if cfg.Logger.Level != "warn" || cfg.Database.Driver != "postgres" || cfg.Server.Protocol != "auto" {
		t.Errorf("normalised: level=%s driver=%s protocol=%s", cfg.Logger.Level, cfg.Database.Driver, cfg.Server.Protocol)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("HYPGO_SERVER_ADDR", ":7001")
	t.Setenv("HYPGO_SERVER_TLS_ENABLED", "true")
	t.Setenv("HYPGO_SERVER_TLS_CERT_FILE", "c.pem")
	t.Setenv("HYPGO_SERVER_TLS_KEY_FILE", "k.pem")
	t.Setenv("HYPGO_SERVER_READ_TIMEOUT", "0")
	t.Setenv("HYPGO_SERVER_WRITE_TIMEOUT", "0") // 檔案裡沒有這個鍵
	t.Setenv("HYPGO_SERVER_TRUSTED_PROXIES", "10.0.0.0/8, 127.0.0.1")
	t.Setenv("HYPGO_DATABASE_DSN", "postgres://env")
	t.Setenv("HYPGO_DATABASE_REDIS_DB", "3")
	t.Setenv("HYPGO_LOGGER_LEVEL", "error")
	cfg, _, err := loadYAML(t, "server:\n  addr: :9000\n  read_timeout: 15\ndatabase:\n  dsn: postgres://file\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":7001" || !cfg.Server.TLS.Enabled || cfg.Server.TLS.CertFile != "c.pem" || cfg.Database.DSN != "postgres://env" ||
		cfg.Database.Redis.DB != 3 || cfg.Logger.Level != "error" || len(cfg.Server.TrustedProxies) != 2 || cfg.Server.TrustedProxies[1] != "127.0.0.1" {
		t.Errorf("env overrides not applied: %+v db=%+v", cfg.Server, cfg.Database)
	}
	// 環境變數給的 0 也是「關閉」
	if cfg.Server.ReadTimeout != 0 || cfg.Server.WriteTimeout != 0 {
		t.Errorf("a 0 from the environment should turn the timeout off, got read=%d write=%d", cfg.Server.ReadTimeout, cfg.Server.WriteTimeout)
	}

	t.Setenv("HYPGO_SERVER_READ_TIMEOUT", "soon")
	if _, _, err := loadYAML(t, "server:\n  addr: :9000\n"); err == nil || !strings.Contains(err.Error(), "HYPGO_SERVER_READ_TIMEOUT") {
		t.Errorf("a malformed value should be an error naming the variable: %v", err)
	}
	t.Setenv("HYPGO_SERVER_READ_TIMEOUT", "5")
	t.Setenv("HYPGO_SERVER_TLS_ENABLED", "perhaps")
	if _, _, err := loadYAML(t, "server:\n  addr: :9000\n"); err == nil {
		t.Error("a malformed boolean should be an error")
	}
}

// .hyp/ 下的設計時設定：打錯的鍵先前同樣沒有反應
func TestDesignConfigsReportUnknownKeys(t *testing.T) {
	var buf bytes.Buffer
	old := WarningOutput
	WarningOutput = &buf
	defer func() { WarningOutput = old }()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		return p
	}

	cc, err := LoadCommentConfig(write("comment.yaml", "ai_coment: true\n"))
	if err != nil || cc.AIComment || !strings.Contains(buf.String(), `did you mean "ai_comment"`) {
		t.Errorf("comment.yaml: cfg=%+v err=%v warnings=%s", cc, err, buf.String())
	}
	buf.Reset()
	if _, err := LoadLLMConfig(write("llm.yaml", "mode: ollama\nollama:\n  model: m\n  time_out: 180\n")); err != nil || !strings.Contains(buf.String(), "ollama.time_out") {
		t.Errorf("llm.yaml: err=%v warnings=%s", err, buf.String())
	}
	buf.Reset()
	if _, err := LoadLLMConfig(write("llm2.yaml", "mod: ollama\n")); err != nil || !strings.Contains(buf.String(), `did you mean "mode"`) {
		t.Errorf("llm.yaml top-level typo: err=%v warnings=%s", err, buf.String())
	}
	buf.Reset()
	if _, err := LoadHypConfig(write("config.yaml", "langauge: en\n")); err != nil || !strings.Contains(buf.String(), "langauge") {
		t.Errorf(".hyp/config.yaml: err=%v warnings=%s", err, buf.String())
	}

	t.Setenv(StrictEnv, "true")
	if _, err := LoadCommentConfig(write("c2.yaml", "ai_coment: true\n")); err == nil {
		t.Error("strict mode should fail on design config typos too")
	}
}

func TestLLMDefaultsAndKeyExpansion(t *testing.T) {
	// Gemini 的 OpenAI 相容端點要帶 /openai：先前預設值少了這一段，pkg/manifest 的請求必定失敗
	for _, base := range []string{"", "https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta/"} {
		c := &LLMConfig{Mode: "api", API: APIConfig{Provider: "gemini", BaseURL: base, Model: "m", APIKey: "k"}}
		c.ApplyDefaults()
		if c.API.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
			t.Errorf("base %q → %q", base, c.API.BaseURL)
		}
	}
	custom := &LLMConfig{Mode: "api", API: APIConfig{Provider: "gemini", BaseURL: "https://proxy.example/v1"}}
	custom.ApplyDefaults()
	if custom.API.BaseURL != "https://proxy.example/v1" {
		t.Errorf("a custom base URL should be kept: %s", custom.API.BaseURL)
	}

	// 只展開 ${NAME}：先前字串裡有 ${ 時，其他的 $abc 也被當成環境變數吃掉
	t.Setenv("HYP_TEST_KEY", "secret")
	for in, want := range map[string]string{
		"${HYP_TEST_KEY}":         "secret",
		"sk-$abc${HYP_TEST_KEY}":  "sk-$abcsecret",
		"sk-$abc":                 "sk-$abc",
		"pre-${HYP_TEST_UNSET}-x": "pre--x",
		"${not valid}":            "${not valid}",
	} {
		if got := (&APIConfig{APIKey: in}).ResolvedAPIKey(); got != want {
			t.Errorf("ResolvedAPIKey(%q) = %q, want %q", in, got, want)
		}
	}
}
