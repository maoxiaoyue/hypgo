package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/secret"
)

// 先前打錯的 --method 先寫進 crypt.yaml 才回錯，之後不帶參數的 init 也一直失敗
func TestSecretInitValidatesMethodBeforeWriting(t *testing.T) {
	root, _ := secretWorkdir(t)
	if _, err := runSecret(t, "", "init", "--method", "aes256"); err == nil || !strings.Contains(err.Error(), "aes256") {
		t.Fatalf("err = %v", err)
	}
	if exists(filepath.Join(root, ".hyp", "crypt.yaml")) {
		t.Fatal("crypt.yaml written with an invalid method")
	}
	if _, err := runSecret(t, "", "init", "--method", "AES-256-GCM"); err != nil {
		t.Fatalf("init after the typo: %v", err)
	}
}

// 先前金鑰在 ~/.hypgo/keys/<專案目錄名>.key：第二個同名專案沿用第一個的金鑰，解得開它的 key.secret
func TestSecretSameNamedProjectsDoNotShareKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(secret.DefaultKeyEnv, "")
	a, b := filepath.Join(t.TempDir(), "api"), filepath.Join(t.TempDir(), "api")
	for _, root := range []string{a, b} {
		os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)
		t.Chdir(root)
		if _, err := runSecret(t, "", "init"); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	if _, err := runSecret(t, "pw-of-A\n", "set", "db_pass"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(a, "key.secret"))
	os.WriteFile(filepath.Join(b, "key.secret"), data, 0o644)
	t.Chdir(b)
	if got, err := runSecret(t, "", "get", "db_pass"); err == nil {
		t.Errorf("project B decrypted project A's key.secret: %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".hypgo")); len(entries) > 0 {
		t.Error("init still writes the key under the home directory")
	}
}

// init 把金鑰檔加進 .gitignore；有 Dockerfile 時 .dockerignore 也加。重跑不重複
func TestSecretInitIgnoresKeyFile(t *testing.T) {
	root, _ := secretWorkdir(t)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("bin/\n"), 0o644)
	os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	for i := 0; i < 2; i++ {
		if _, err := runSecret(t, "", "init"); err != nil {
			t.Fatal(err)
		}
	}
	for name, prefix := range map[string]string{".gitignore": "bin/\n\n", ".dockerignore": ""} {
		data, _ := os.ReadFile(filepath.Join(root, name))
		if string(data) != prefix+"# hyp secret 的金鑰檔：不可進版控\n.hyp/secret.*\n" {
			t.Errorf("%s = %q", name, data)
		}
	}

	// 沒有 Dockerfile 的專案不產生 .dockerignore
	root, _ = secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, ".dockerignore")) {
		t.Error(".dockerignore created without a Dockerfile")
	}
	if !exists(filepath.Join(root, ".gitignore")) {
		t.Error(".gitignore not created")
	}
}

// 金鑰檔被 git 追蹤時 hyp secret 拒絕執行；取消追蹤後恢復
func TestSecretRefusesTrackedKeyFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, _ := secretWorkdir(t)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "v\n", "set", "a"); err != nil {
		t.Fatalf("ignored key file should be fine: %v", err)
	}

	git("add", "-f", ".hyp/secret.key")
	for _, args := range [][]string{{"list"}, {"get", "a"}, {"set", "b"}, {"init"}, {"--restore"}} {
		_, err := runSecret(t, "v\n", args...)
		if err == nil || !strings.Contains(err.Error(), "tracked by git") || !strings.Contains(err.Error(), "git rm --cached .hyp/secret.key") {
			t.Errorf("hyp secret %v with a tracked key: %v", args, err)
		}
	}
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "config.yaml"), []byte("db:\n  password: x\n"), 0o644)
	if err := runHyp(t, "crypt", "--key", "db"); err == nil || !strings.Contains(err.Error(), "tracked by git") {
		t.Errorf("hyp crypt with a tracked key: %v", err)
	}

	git("rm", "-q", "--cached", ".hyp/secret.key")
	if _, err := runSecret(t, "", "get", "a"); err != nil {
		t.Errorf("after git rm --cached: %v", err)
	}
}

// 仍在使用舊位置的金鑰時提示搬移（不回錯）
func TestSecretNotesLegacyKeyLocation(t *testing.T) {
	root, home := secretWorkdir(t)
	key, _ := secret.GenerateKey()
	legacy := filepath.Join(home, ".hypgo", "keys", "myapp.key")
	if err := secret.WriteKeyFile(legacy, key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, key)
	if _, err := runSecret(t, "v\n", "set", "a"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, "")
	_ = root

	out, err := runHypOutput(t, "secret", "get", "a")
	if err != nil || !strings.Contains(out, "v\n") {
		t.Fatalf("get with the legacy key: %q, %v", out, err)
	}
	if !strings.Contains(out, "Note: using the key at "+legacy) || !strings.Contains(out, ".hyp/secret.key") {
		t.Errorf("no note about the legacy location: %q", out)
	}
}

// hyp secret --restore 把設定檔裡的引用換回明文，其餘內容不動，key.secret 不變
func TestSecretRestore(t *testing.T) {
	root, _ := secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"db_user": "app",
		"db_pass": `p@ss "quoted" \back # hash: 'x'`,
		"redis":   "r3dis",
		"num":     "123456",
	} {
		if _, err := runSecret(t, value+"\n", "set", name); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, "config", "extra"), 0o755)
	main := filepath.Join(root, "config", "config.yaml")
	os.WriteFile(main, []byte("# 設定\r\n"+
		"database:\r\n"+
		"  dsn: \"${secret:db_user}:${secret:db_pass}@tcp(db:3306)/app\"   # 連線\r\n"+
		"  redis:\r\n"+
		"    password: '${secret:redis}'\r\n"+
		"    db: 0\r\n"+
		"  pin: ${secret:num}\r\n"+
		"  list: [\"${secret:redis}\", plain]\r\n"), 0o644)
	other := filepath.Join(root, "config", "extra", "queue.yml")
	os.WriteFile(other, []byte("queue:\n  token: \"${secret:redis}\"\n"), 0o644)
	untouched := filepath.Join(root, "config", "plain.yaml")
	os.WriteFile(untouched, []byte("a:   1 # 沒有引用\n"), 0o644)
	sealed, _ := os.ReadFile(filepath.Join(root, "key.secret"))

	out, err := runSecret(t, "", "--restore")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "r3dis") || strings.Contains(out, "p@ss") {
		t.Errorf("--restore printed a value: %q", out)
	}
	want := "# 設定\r\n" +
		"database:\r\n" +
		"  dsn: \"app:p@ss \\\"quoted\\\" \\\\back # hash: 'x'@tcp(db:3306)/app\"   # 連線\r\n" +
		"  redis:\r\n" +
		"    password: \"r3dis\"\r\n" +
		"    db: 0\r\n" +
		"  pin: \"123456\"\r\n" +
		"  list: [\"r3dis\", plain]\r\n"
	if got, _ := os.ReadFile(main); string(got) != want {
		t.Errorf("config.yaml =\n%q\nwant\n%q", got, want)
	}
	if got, _ := os.ReadFile(other); string(got) != "queue:\n  token: \"r3dis\"\n" {
		t.Errorf("queue.yml = %q", got)
	}
	if got, _ := os.ReadFile(untouched); string(got) != "a:   1 # 沒有引用\n" {
		t.Errorf("file without references was rewritten: %q", got)
	}
	if now, _ := os.ReadFile(filepath.Join(root, "key.secret")); string(now) != string(sealed) {
		t.Error("key.secret changed")
	}

	// 再跑一次：沒有引用了
	if out, err := runSecret(t, "", "--restore"); err != nil || !strings.Contains(out, "nothing changed") {
		t.Errorf("second restore: %q, %v", out, err)
	}
}

// 有一個引用解不開（或寫法無法安全改寫）時，任何檔案都不動
func TestSecretRestoreIsAllOrNothing(t *testing.T) {
	root, _ := secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "v\n", "set", "known"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	a, b := filepath.Join(root, "config", "a.yaml"), filepath.Join(root, "config", "b.yaml")
	const aText, bText = "x: \"${secret:known}\"\n", "y: \"${secret:missing}\"\n"
	os.WriteFile(a, []byte(aText), 0o644)
	os.WriteFile(b, []byte(bText), 0o644)
	if _, err := runSecret(t, "", "--restore"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(a); string(got) != aText {
		t.Errorf("a.yaml was rewritten although b.yaml failed: %q", got)
	}

	// 多行的值無法只改一行：回錯、不動檔案
	const block = "x: |\n  line ${secret:known}\n  more\n"
	os.WriteFile(b, []byte(block), 0o644)
	if _, err := runSecret(t, "", "--restore"); err == nil {
		t.Error("block scalar should be refused")
	}
	if got, _ := os.ReadFile(a); string(got) != aText {
		t.Errorf("a.yaml was rewritten: %q", got)
	}

	// --file 只處理指定的檔案
	if _, err := runSecret(t, "", "--restore", "--file", a); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(a); string(got) != "x: \"v\"\n" {
		t.Errorf("a.yaml = %q", got)
	}
	if got, _ := os.ReadFile(b); string(got) != block {
		t.Errorf("b.yaml was rewritten: %q", got)
	}
}

// keygen 寫到預設位置（專案內）時也先確保它被忽略；-o 指到別處時不動 .gitignore
func TestSecretKeygenIgnoresDefaultKeyFile(t *testing.T) {
	root, home := secretWorkdir(t)
	if _, err := runSecret(t, "", "keygen", "-o", filepath.Join(home, "elsewhere.key")); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, ".gitignore")) {
		t.Error("keygen -o outside the project touched .gitignore")
	}
	if _, err := runSecret(t, "", "keygen"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(data), secret.KeyIgnorePattern) || !exists(filepath.Join(root, ".hyp", "secret.key")) {
		t.Errorf(".gitignore = %q", data)
	}
}

// 換兩次金鑰：第一把舊金鑰（.old）不能被第二把蓋掉
func TestSecretRotateTwiceKeepsEveryOldKey(t *testing.T) {
	root, home := secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "v\n", "set", "a"); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, ".hyp", "secret.key")
	first, _ := os.ReadFile(keyPath)
	for _, name := range []string{"k1.key", "k2.key"} {
		next := filepath.Join(home, name)
		if _, err := runSecret(t, "", "keygen", "-o", next); err != nil {
			t.Fatal(err)
		}
		if _, err := runSecret(t, "", "rotate", "--new-key-file", next); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := runSecret(t, "", "get", "a"); err != nil || got != "v\n" {
		t.Errorf("get after two rotations = %q, %v", got, err)
	}
	if old, _ := os.ReadFile(keyPath + ".old"); string(old) != string(first) {
		t.Error("the first key (.old) was overwritten by the second rotation")
	}
	if matches, _ := filepath.Glob(keyPath + ".old.*"); len(matches) != 1 {
		t.Errorf("second old key not kept: %v", matches)
	}
}

// hyp api --force：既有的 .dockerignore 保留並補上金鑰檔的樣式
func TestAPIKeepsExistingDockerignore(t *testing.T) {
	offlineGo(t)
	t.Chdir(t.TempDir())
	os.Mkdir("svc", 0o755)
	os.WriteFile(filepath.Join("svc", ".dockerignore"), []byte(".git\n"), 0o644)
	if err := runHyp(t, "api", "svc", "--force"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join("svc", ".dockerignore")); string(data) != ".git\n\n# hyp secret 的金鑰檔：不可進版控\n.hyp/secret.*\n" {
		t.Errorf(".dockerignore = %q", data)
	}
}

// 舊位置（~/.hypgo/keys/<目錄名>.key）的金鑰由同名專案共用：rotate 不能動它，新金鑰放進專案的 .hyp/。
// 歷史 bug：rotate 把它改名為 .old 並寫入新金鑰，另一個同名專案從此回報 key does not match
func TestSecretRotateLeavesSharedLegacyKeyAlone(t *testing.T) {
	root, home := secretWorkdir(t)
	key, _ := secret.GenerateKey()
	legacy := filepath.Join(home, ".hypgo", "keys", "myapp.key")
	if err := secret.WriteKeyFile(legacy, key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, key)
	if _, err := runSecret(t, "v\n", "set", "a"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultKeyEnv, "")
	before, _ := os.ReadFile(legacy)

	next := filepath.Join(root, ".hyp", "secret.new")
	if _, err := runSecret(t, "", "keygen", "-o", next); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "rotate", "--new-key-file", next); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(legacy); err != nil || string(after) != string(before) {
		t.Errorf("the shared legacy key was changed (err %v)", err)
	}
	if _, err := os.Stat(legacy + ".old"); err == nil {
		t.Error("the shared legacy key was renamed to .old")
	}
	if _, err := os.Stat(filepath.Join(root, ".hyp", "secret.key")); err != nil {
		t.Errorf("the new key is not in the project: %v", err)
	}
	if got, err := runSecret(t, "", "get", "a"); err != nil || got != "v\n" {
		t.Errorf("get after rotate = %q, %v", got, err)
	}
	if ignore, _ := os.ReadFile(filepath.Join(root, ".gitignore")); !strings.Contains(string(ignore), secret.KeyIgnorePattern) {
		t.Errorf(".gitignore does not cover the new key: %q", ignore)
	}
}

// --restore 把明文寫回被 git 追蹤的設定檔時指名警告。歷史 bug：只有一句籠統的「不要提交」
func TestSecretRestoreWarnsAboutTrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, _ := secretWorkdir(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "p@ss\n", "set", "db_pass"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	for _, name := range []string{"config.yaml", "local.yaml"} {
		os.WriteFile(filepath.Join(root, "config", name), []byte("database:\n  password: ${secret:db_pass}\n"), 0o644)
	}
	if out, err := exec.Command("git", "-C", root, "add", "config/config.yaml").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	out, err := runHypOutput(t, "secret", "--restore")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "config/config.yaml is tracked by git") {
		t.Errorf("no warning for the tracked file: %q", out)
	}
	if strings.Contains(out, "local.yaml is tracked") {
		t.Errorf("warned about an untracked file: %q", out)
	}
}
