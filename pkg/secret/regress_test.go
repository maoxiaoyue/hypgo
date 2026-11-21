package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 先前預設金鑰路徑是 ~/.hypgo/keys/<專案目錄名>.key：兩個目錄同名的專案共用同一把金鑰
func TestDefaultKeyPathIsPerProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a, b := filepath.Join(t.TempDir(), "api"), filepath.Join(t.TempDir(), "api")
	var paths []string
	for _, root := range []string{a, b} {
		os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
		for _, get := range []func(*Settings) (string, error){(*Settings).KeyFilePath, (*Settings).IdentityFilePath} {
			p, err := get(mustSettings(t, root))
			if err != nil {
				t.Fatal(err)
			}
			if rel, err := filepath.Rel(root, p); err != nil || !strings.HasPrefix(filepath.ToSlash(rel), ".hyp/secret.") {
				t.Errorf("key path %s is not .hyp/secret.* of %s", p, root)
			}
			paths = append(paths, p)
		}
	}
	if paths[0] == paths[2] || paths[1] == paths[3] {
		t.Errorf("two projects named api share a key file: %v", paths)
	}
}

// 既有專案（已有 key.secret、金鑰在舊位置）繼續可用；沒有 key.secret 的新專案不沿用舊位置的金鑰
func TestLegacyKeyPathFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(DefaultKeyEnv, "")
	root := filepath.Join(t.TempDir(), "api")
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
	s := mustSettings(t, root)
	legacy := filepath.Join(home, ".hypgo", "keys", "api.key")
	key, _ := GenerateKey()
	if err := WriteKeyFile(legacy, key); err != nil {
		t.Fatal(err)
	}
	inProject := filepath.Join(root, ".hyp", "secret.key")

	if p, _ := s.KeyFilePath(); p != inProject {
		t.Errorf("new project picked up another project's key: %s", p)
	}

	// 以舊位置的金鑰寫出 key.secret，模擬升級前的專案
	t.Setenv(DefaultKeyEnv, key)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DefaultKeyEnv, "")
	if p, _ := s.KeyFilePath(); p != legacy {
		t.Errorf("existing project should keep using %s, got %s", legacy, p)
	}
	if v, err := mustOpen(t, s).Get("a"); err != nil || v != testPlain {
		t.Errorf("Get with the legacy key: %q, %v", v, err)
	}

	// 專案內有金鑰檔時以它為準
	other, _ := GenerateKey()
	if err := WriteKeyFile(inProject, other); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.KeyFilePath(); p != inProject {
		t.Errorf("project key should win: %s", p)
	}
}

// 先前寫錯的引用載入成功，字面內容直接當成密碼
func TestMalformedReferenceIsAnError(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("db_pass", "has $secret: and ${SECRET:x} inside"); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(s.Root(), "config", "config.yaml")
	for _, ref := range []string{"${secret:db_pass", "${SECRET:db_pass}", "$secret:db_pass", "${secrets:db_pass}", "${ secret:db_pass}", "${secret :db_pass}"} {
		cfg := &expandCfg{DSN: "app:" + ref + "@db/app"}
		err := ExpandConfig(cfg, configFile)
		if err == nil || !strings.Contains(err.Error(), "DSN") || !strings.Contains(err.Error(), "${secret:name}") {
			t.Errorf("%s: err = %v, dsn = %q", ref, err, cfg.DSN)
		}
	}
	for _, ref := range []string{"${secret: db_pass}", "${secret:db_pass }"} {
		err := ExpandConfig(&expandCfg{DSN: ref}, configFile)
		if err == nil || !strings.Contains(err.Error(), "spaces") {
			t.Errorf("%s: err = %v", ref, err)
		}
	}
	// 只檢查設定檔的原文：解密出來的值長得像引用不算錯，也不再展開
	cfg := &expandCfg{DSN: "${secret:db_pass}", Plain: "$HOME ${OTHER} secret: x"}
	if err := ExpandConfig(cfg, configFile); err != nil || cfg.DSN != "has $secret: and ${SECRET:x} inside" {
		t.Errorf("valid reference: %q, %v", cfg.DSN, err)
	}
}

// 先前兩個行程各自開啟後寫入，先寫的那筆消失
func TestSetFromTwoOpenersKeepsBoth(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("base", "1"); err != nil {
		t.Fatal(err)
	}
	a, b, c := mustOpen(t, s), mustOpen(t, s), mustOpen(t, s)
	if err := a.Set("from_a", "x"); err != nil {
		t.Fatal(err)
	}
	if err := b.Set("from_b", "y"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete("base"); err != nil {
		t.Fatal(err)
	}
	got := mustOpen(t, s)
	if names := strings.Join(got.Names(), ","); names != "from_a,from_b" {
		t.Errorf("names = %s, want from_a,from_b", names)
	}
	for name, want := range map[string]string{"from_a": "x", "from_b": "y"} {
		if v, err := got.Get(name); err != nil || v != want {
			t.Errorf("%s = %q, %v", name, v, err)
		}
	}
	if _, err := os.Stat(s.Path() + ".lock"); err == nil {
		t.Error("lock file left behind")
	}
}

// 另一個行程換了金鑰之後，手上還是舊金鑰的行程不能把兩把金鑰的項目混在同一個檔案
func TestSetAfterKeyChangedOnDisk(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	stale := mustOpen(t, s)
	if _, err := stale.Get("a"); err != nil { // 取得舊金鑰
		t.Fatal(err)
	}
	newKey, _ := GenerateKey()
	raw, _ := DecodeKey(newKey)
	if _, err := Rotate(s, Options{}, RotateOptions{NewKey: raw}); err != nil {
		t.Fatal(err)
	}
	if err := stale.Set("b", "2"); err == nil || !strings.Contains(err.Error(), "changed by another process") {
		t.Fatalf("Set with the old key: %v", err)
	}
	t.Setenv(DefaultKeyEnv, newKey)
	if names := mustOpen(t, s).Names(); len(names) != 1 {
		t.Errorf("names = %v", names)
	}
}

func TestLockFile(t *testing.T) {
	s := newProject(t, MethodAES)
	lock := s.Path() + ".lock"
	defer func(d time.Duration) { lockWait = d }(lockWait)
	lockWait = 150 * time.Millisecond

	// 有人持有鎖：等到期限後回錯，不寫入
	os.WriteFile(lock, nil, 0o600)
	if err := mustOpen(t, s).Set("a", "1"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("Set while locked: %v", err)
	}
	if _, err := os.Stat(s.Path()); err == nil {
		t.Error("key.secret written while locked")
	}
	// 行程中斷留下的舊鎖檔不能永遠擋住
	old := time.Now().Add(-2 * lockStale)
	os.Chtimes(lock, old, old)
	if err := mustOpen(t, s).Set("a", "1"); err != nil {
		t.Fatalf("Set with a stale lock: %v", err)
	}
}

func TestEnsureIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const block = "# hyp secret 的金鑰檔：不可進版控\n.hyp/secret.*\n"
	check := func(wantAdded bool, want string) {
		t.Helper()
		added, err := EnsureIgnored(path)
		if err != nil || added != wantAdded {
			t.Fatalf("EnsureIgnored = %v, %v", added, err)
		}
		if data, _ := os.ReadFile(path); string(data) != want {
			t.Errorf("content = %q, want %q", data, want)
		}
	}
	check(true, block)  // 不存在時建立
	check(false, block) // 不重複加

	os.WriteFile(path, []byte("bin/\n*.log"), 0o644) // 結尾沒有換行
	check(true, "bin/\n*.log\n\n"+block)

	os.WriteFile(path, []byte("bin/\r\n"), 0o644) // 保留 CRLF
	check(true, "bin/\r\n\r\n"+strings.ReplaceAll(block, "\n", "\r\n"))

	os.WriteFile(path, []byte("bin/\n  .hyp/secret.*  \n"), 0o644)
	check(false, "bin/\n  .hyp/secret.*  \n")
}

// rotate 解密與輸入新密碼的期間不持有鎖；這段時間檔案被別的行程改過時不能寫入，否則那筆改動被蓋掉
func TestRotateRefusesFileChangedMeanwhile(t *testing.T) {
	s := newProject(t, MethodPassphrase)
	if err := mustOpen(t, s).Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path())
	prompt := func(bool) (string, error) {
		// 另一個行程在這時寫入一筆
		if err := mustOpen(t, s).Set("added_meanwhile", "2"); err != nil {
			t.Fatal(err)
		}
		return "a brand new passphrase", nil
	}
	if _, err := Rotate(s, Options{Prompt: prompt}, RotateOptions{}); err == nil || !strings.Contains(err.Error(), "changed by another process") {
		t.Fatalf("Rotate = %v", err)
	}
	got := mustOpen(t, s)
	if names := strings.Join(got.Names(), ","); names != "a,added_meanwhile" {
		t.Errorf("names = %s (before: %d bytes)", names, len(before))
	}
	if v, err := got.Get("added_meanwhile"); err != nil || v != "2" {
		t.Errorf("the concurrent write was lost or re-encrypted: %q, %v", v, err)
	}
}
