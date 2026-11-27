package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRestartRefusesUnlessGracefulRestartEnabled：只有確定啟用了 graceful restart 才送訊號。
// 先前只在設定檔「有寫且為 false」時才擋：沒寫這個鍵（預設 false）、設定檔在別的位置、
// 以環境變數關閉時照送 SIGUSR2，沒啟用的 server 不接這個訊號，行程被結束
func TestRestartRefusesUnlessGracefulRestartEnabled(t *testing.T) {
	write := func(t *testing.T, path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	on := "server:\n  enable_graceful_restart: true\n"
	off := "server:\n  enable_graceful_restart: false\n"
	cases := []struct {
		name    string
		path    string // 空字串 = 沒有設定檔
		body    string
		env     string
		enabled bool
		why     string // 依據裡要出現的字
	}{
		{name: "key missing", path: "config/config.yaml", body: "server:\n  addr: :8080\n", why: "does not set"},
		{name: "false", path: "config/config.yaml", body: off, why: "is false"},
		{name: "true", path: "config/config.yaml", body: on, enabled: true},
		{name: "true in root config.yaml", path: "config.yaml", body: on, enabled: true},
		{name: "true in app/config", path: "app/config/config.yaml", body: on, enabled: true},
		{name: "no config file", why: "no config file"},
		{name: "broken yaml", path: "config/config.yaml", body: "server: [", why: "cannot be parsed"},
		{name: "env turns it off", path: "config/config.yaml", body: on, env: "false", why: restartEnvKey},
		{name: "env turns it on", path: "config/config.yaml", body: off, env: "true", enabled: true},
		{name: "empty env is unset", path: "config/config.yaml", body: on, env: "", enabled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv(restartEnvKey, tc.env)
			if tc.path != "" {
				write(t, tc.path, tc.body)
			}
			enabled, why := gracefulRestartEnabled()
			if enabled != tc.enabled || !strings.Contains(why, tc.why) {
				t.Errorf("gracefulRestartEnabled() = %v, %q; want %v with %q", enabled, why, tc.enabled, tc.why)
			}
			err := checkGracefulRestart(4242, false)
			if tc.enabled && err != nil {
				t.Errorf("should allow the restart, got %v", err)
			}
			if !tc.enabled && (err == nil || !strings.Contains(err.Error(), "4242") || !strings.Contains(err.Error(), "--force")) {
				t.Errorf("should refuse, naming the PID and --force; got %v", err)
			}
			if err := checkGracefulRestart(4242, true); err != nil {
				t.Errorf("--force should skip the check, got %v", err)
			}
		})
	}
}

// TestSameDir：PID 檔記的行程是否在這個目錄執行，以解開符號連結後的路徑比較
func TestSameDir(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if !sameDir(a, filepath.Join(a, "sub", "..")) {
		t.Error("the same directory spelled differently should match")
	}
	if sameDir(a, b) {
		t.Error("different directories should not match")
	}
}
