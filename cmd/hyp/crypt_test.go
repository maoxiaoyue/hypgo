package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// cryptProject 建立已有金鑰的專案，並寫入 config/config.yaml
func cryptProject(t *testing.T, content string) (root, cfgPath string) {
	t.Helper()
	root, _ = secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(root, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cfgPath
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCryptEncryptsAndKeepsTheRestOfTheFile(t *testing.T) {
	const before = "# 伺服器\nserver:\n    addr: \":8080\"   # 對齊的註解\n\ndatabase:\n  driver: postgres\n  postgresql:\n    host: db\n    user: app_user\n    password: \"s3cr3t \\\"quoted\\\"\"  # 正式環境\n\n  mysql:\n    user: other\n"
	const after = "# 伺服器\nserver:\n    addr: \":8080\"   # 對齊的註解\n\ndatabase:\n  driver: postgres\n  postgresql:\n    host: db\n    user: \"${secret:database.postgresql.user}\"\n    password: \"${secret:database.postgresql.password}\"  # 正式環境\n\n  mysql:\n    user: other\n"
	root, cfgPath := cryptProject(t, before)

	out, err := runHypOutput(t, "crypt", "--key", "database.postgresql") // --file 預設 config/config.yaml
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, cfgPath); got != after {
		t.Errorf("config.yaml:\n%s", got)
	}
	sealed := mustRead(t, filepath.Join(root, "key.secret"))
	for _, plain := range []string{"app_user", "s3cr3t"} {
		if strings.Contains(sealed, plain) || strings.Contains(out, plain) {
			t.Errorf("%q leaked (output: %s)", plain, out)
		}
	}
	if got, err := runSecret(t, "", "get", "database.postgresql.password"); err != nil || got != "s3cr3t \"quoted\"\n" {
		t.Errorf("password = %q, %v", got, err)
	}

	// 執行期：使用者自訂的結構讀得到明文
	var cfg struct {
		Database struct {
			PostgreSQL struct {
				User     string `yaml:"user"`
				Password string `yaml:"password"`
			} `yaml:"postgresql"`
		} `yaml:"database"`
	}
	if err := config.NewConfigLoader("").Load(cfgPath, &cfg); err != nil {
		t.Fatal(err)
	}
	if p := cfg.Database.PostgreSQL; p.User != "app_user" || p.Password != "s3cr3t \"quoted\"" {
		t.Errorf("loaded %+v", p)
	}
}

func TestCryptValueForms(t *testing.T) {
	for name, tc := range map[string]struct{ config, want, field, plain string }{
		"username passwd":    {"db:\n  username: u\n  passwd: 'it''s'\n", "db:\n  username: \"${secret:db.username}\"\n  passwd: \"${secret:db.passwd}\"\n", "db.passwd", "it's"},
		"numeric keeps text": {"db:\n  password: 0123\n", "db:\n  password: \"${secret:db.password}\"\n", "db.password", "0123"},
		"crlf":               {"db:\r\n  password: abc\r\n", "db:\r\n  password: \"${secret:db.password}\"\r\n", "db.password", "abc"},
		"flow":               {"db: {user: 甲, password: b}\n", "db: {user: \"${secret:db.user}\", password: \"${secret:db.password}\"}\n", "db.user", "甲"},
		"--file elsewhere":   {"db:\n  PASSWORD: x#y\n", "db:\n  PASSWORD: \"${secret:db.PASSWORD}\"\n", "db.PASSWORD", "x#y"},
	} {
		t.Run(name, func(t *testing.T) {
			_, cfgPath := cryptProject(t, tc.config)
			if _, err := runHypOutput(t, "crypt", "--file", cfgPath, "--key", "db"); err != nil {
				t.Fatal(err)
			}
			if got := mustRead(t, cfgPath); got != tc.want {
				t.Errorf("config = %q", got)
			}
			if got, err := runSecret(t, "", "get", tc.field); err != nil || got != tc.plain+"\n" {
				t.Errorf("%s = %q, %v", tc.field, got, err)
			}
		})
	}
}

func TestCryptSkipsWhatIsNotPlaintext(t *testing.T) {
	const content = "db:\n  user: \"${secret:db.user}\"\n  password: \"${DB_PASS}\"\n  passwd:\n  username: \"\"\n"
	root, cfgPath := cryptProject(t, content)

	out, err := runHypOutput(t, "crypt", "--key", "db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to encrypt") {
		t.Errorf("output: %s", out)
	}
	if got := mustRead(t, cfgPath); got != content {
		t.Errorf("config changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "key.secret")); err == nil {
		t.Error("key.secret created with nothing to encrypt")
	}
}

func TestCryptRunTwice(t *testing.T) {
	_, cfgPath := cryptProject(t, "db:\n  password: abc\n")
	for i := 0; i < 2; i++ {
		if _, err := runHypOutput(t, "crypt", "--key", "db"); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := runSecret(t, "", "get", "db.password"); err != nil || got != "abc\n" {
		t.Errorf("second run corrupted the value: %q, %v (config: %s)", got, err, mustRead(t, cfgPath))
	}

	// 改回新的明文再跑：key.secret 要更新，並明確說是取代
	if err := os.WriteFile(cfgPath, []byte("db:\n  password: new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHypOutput(t, "crypt", "--key", "db")
	if err != nil || !strings.Contains(out, "Replaced db.password") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if got, _ := runSecret(t, "", "get", "db.password"); got != "new\n" {
		t.Errorf("value = %q", got)
	}
}

// 所有失敗都不能動到設定檔與 key.secret，也不能把值印出來
func TestCryptErrorsChangeNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		args   []string
		want   string
	}{
		"missing --key":  {"db:\n  password: plainvalue\n", nil, "--key is required"},
		"key not found":  {"db:\n  password: plainvalue\n", []string{"--key", "database.postgresql"}, "not found"},
		"not a section":  {"db:\n  password: plainvalue\n", []string{"--key", "db.password"}, "not a section"},
		"no credentials": {"db:\n  dsn: plainvalue\n", []string{"--key", "db"}, "no user"},
		"block scalar":   {"db:\n  user: ok\n  password: |\n    plainvalue\n", []string{"--key", "db"}, "not a plain one-line value"},
		"multi-line":     {"db:\n  password: plain\n    value\n", []string{"--key", "db"}, "not a plain one-line value"},
		"alias value":    {"x: &p plainvalue\ndb:\n  password: *p\n", []string{"--key", "db"}, "single value"},
		"alias section":  {"x: &p\n  password: plainvalue\ndb: *p\n", []string{"--key", "db"}, "alias"},
		"tagged":         {"db:\n  password: !!str plainvalue\n", []string{"--key", "db"}, "not a plain one-line value"},
		"invalid yaml":   {"db: [\n", []string{"--key", "db"}, "config.yaml"},
		"empty file":     {"", []string{"--key", "db"}, "not found"},
		"invalid name":   {"a b:\n  password: plainvalue\n", []string{"--key", "a b"}, "invalid name"},
		// YAML 把註解裡的 U+2028 也算換行，行號差一行：不擋的話會改到下一行的 secret
		"line numbers off": {"# x\u2028\ndb:\n  passwd: plainvalue\n  secret: plainvalue\n", []string{"--key", "db"}, "could not update the file safely"},
	} {
		t.Run(name, func(t *testing.T) {
			root, cfgPath := cryptProject(t, tc.config)
			out, err := runHypOutput(t, append([]string{"crypt"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if strings.Contains(out, "plainvalue") || strings.Contains(err.Error(), "plainvalue") {
				t.Errorf("value leaked: %q, %v", out, err)
			}
			if got := mustRead(t, cfgPath); got != tc.config {
				t.Errorf("config changed: %q", got)
			}
			if _, err := os.Stat(filepath.Join(root, "key.secret")); err == nil {
				t.Error("key.secret was written")
			}
		})
	}
}

func TestCryptWithoutKeyLeavesPlaintextInPlace(t *testing.T) {
	root, _ := secretWorkdir(t)
	cfgPath := filepath.Join(root, "c.yaml")
	const content = "db:\n  password: abc\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runHypOutput(t, "crypt", "--file", cfgPath, "--key", "db")
	if err == nil || !strings.Contains(err.Error(), "hyp secret init") {
		t.Fatalf("err = %v", err)
	}
	if got := mustRead(t, cfgPath); got != content {
		t.Errorf("config changed without a key: %q", got)
	}
}

// 根目錄由設定檔的位置決定（與載入時相同），不是工作目錄：否則執行期找不到 key.secret
func TestCryptFindsProjectFromTheFile(t *testing.T) {
	root, cfgPath := cryptProject(t, "database:\n  redis:\n    password: abc\n")
	t.Chdir(t.TempDir())
	if _, err := runHypOutput(t, "crypt", "--file", cfgPath, "--key", "database.redis"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "key.secret")); err != nil {
		t.Fatalf("key.secret not in the project: %v", err)
	}
	// 框架自己的欄位：config.LoadConfig 就會解密
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Redis.Password != "abc" {
		t.Errorf("password = %q", cfg.Database.Redis.Password)
	}
}

func TestCryptMissingFile(t *testing.T) {
	secretWorkdir(t)
	if _, err := runHypOutput(t, "crypt", "--key", "db"); err == nil {
		t.Fatal("missing default config/config.yaml should fail")
	}
}
