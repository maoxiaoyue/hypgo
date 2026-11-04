package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/secret"
)

// secretProject 建立含 key.secret 的專案，回傳 config/config.yaml 的路徑
func secretProject(t *testing.T, yaml string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	key, err := secret.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, key)
	for _, dir := range []string{".hyp", "config"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := secret.LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := secret.Open(settings, secret.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("db_pass", "p@ss:w/rd"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigExpandsSecretReferences(t *testing.T) {
	path := secretProject(t, `
database:
  driver: mysql
  dsn: "app:${secret:db_pass}@tcp(db:3306)/app"
  redis:
    password: "${secret:db_pass}"
  replicas:
    - dsn: "ro:${secret:db_pass}@tcp(ro:3306)/app"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "app:p@ss:w/rd@tcp(db:3306)/app"; cfg.Database.DSN != want {
		t.Errorf("DSN = %q, want %q", cfg.Database.DSN, want)
	}
	if cfg.Database.Redis.Password != "p@ss:w/rd" {
		t.Errorf("Redis.Password = %q", cfg.Database.Redis.Password)
	}
	if want := "ro:p@ss:w/rd@tcp(ro:3306)/app"; cfg.Database.Replicas[0].DSN != want {
		t.Errorf("Replicas[0].DSN = %q", cfg.Database.Replicas[0].DSN)
	}
}

func TestLoadConfigFailsWhenSecretUnavailable(t *testing.T) {
	path := secretProject(t, "database:\n  dsn: \"${secret:db_pass}\"\n")
	t.Setenv(secret.DefaultKeyEnv, "")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "Database.DSN") {
		t.Fatalf("err = %v, want it to name the config field", err)
	}

	path = secretProject(t, "database:\n  dsn: \"${secret:nope}\"\n")
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown name: err = %v", err)
	}
}

// 沒有用 ${secret:} 的專案不需要金鑰，也不會受 key.secret 影響
func TestLoadConfigWithoutSecretsNeedsNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("database:\n  dsn: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, "")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.DSN != "plain" {
		t.Errorf("DSN = %q", cfg.Database.DSN)
	}
}
