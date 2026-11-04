package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/maoxiaoyue/hypgo/pkg/secret"
)

// runSecret 執行 hyp secret 子命令，回傳 stdout；stdin 為機密值的輸入來源
func runSecret(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	defer func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		resetFlags(rootCmd)
	}()
	rootCmd.SetArgs(append([]string{"secret"}, args...))
	err := rootCmd.Execute()
	return out.String(), err
}

// secretWorkdir 切到暫存專案；家目錄也指向暫存處，測試不會碰到使用者真正的金鑰
func secretWorkdir(t *testing.T) (root, home string) {
	t.Helper()
	root, home = filepath.Join(t.TempDir(), "myapp"), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".hyp"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, env := range []string{secret.DefaultKeyEnv, secret.DefaultPassphraseEnv, secret.DefaultAgeIdentityEnv, secret.NewPassphraseEnv} {
		t.Setenv(env, "")
	}
	return root, home
}

func TestSecretSymmetricWorkflow(t *testing.T) {
	root, home := secretWorkdir(t)

	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".hyp", "crypt.yaml")); err != nil {
		t.Fatalf("crypt.yaml not created: %v", err)
	}
	keyPath := filepath.Join(home, ".hypgo", "keys", "myapp.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("key file not created at %s: %v", keyPath, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(keyPath); info.Mode().Perm() != 0o600 {
			t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
		}
	}

	// init 再跑一次不能換掉金鑰：否則既有的 key.secret 永遠解不開
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(keyPath); string(again) != string(key) {
		t.Fatal("second init replaced the key")
	}

	out, err := runSecret(t, "p@ss word\n", "set", "db_pass")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "p@ss word") {
		t.Errorf("set echoed the value: %q", out)
	}
	// 只移除結尾的換行，值裡的空白要保留
	if got, err := runSecret(t, "", "get", "db_pass"); err != nil || got != "p@ss word\n" {
		t.Errorf("get = %q, %v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "key.secret"))
	if strings.Contains(string(raw), "p@ss word") {
		t.Error("key.secret contains the plaintext")
	}

	list, err := runSecret(t, "", "list")
	if err != nil || !strings.Contains(list, "db_pass") || !strings.Contains(list, secret.MethodAES) {
		t.Errorf("list = %q, %v", list, err)
	}
	if strings.Contains(list, "p@ss") {
		t.Errorf("list shows the value: %q", list)
	}

	// 換金鑰：舊金鑰解不開，新金鑰可以
	newKey := filepath.Join(home, "new.key")
	if _, err := runSecret(t, "", "keygen", "-o", newKey); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "keygen", "-o", newKey); err == nil {
		t.Error("keygen overwrote an existing key file")
	}
	if _, err := runSecret(t, "", "rotate", "--new-key-file", newKey); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "get", "db_pass"); err == nil {
		t.Error("old key still decrypts after rotate")
	}
	data, _ := os.ReadFile(newKey)
	t.Setenv(secret.DefaultKeyEnv, strings.TrimSpace(string(data)))
	if got, err := runSecret(t, "", "get", "db_pass"); err != nil || got != "p@ss word\n" {
		t.Errorf("get with new key = %q, %v", got, err)
	}

	if _, err := runSecret(t, "", "rm", "db_pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "get", "db_pass"); err == nil {
		t.Error("get after rm should fail")
	}
}

// 值不能從命令列參數傳入：會留在 shell 歷史與行程清單
func TestSecretSetRefusesValueArgument(t *testing.T) {
	secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "set", "db_pass", "hunter2"); err == nil {
		t.Error("set accepted the value as an argument")
	}
	if _, err := runSecret(t, "\n", "set", "db_pass"); err == nil {
		t.Error("set accepted an empty value")
	}
}

func TestSecretAgeWorkflow(t *testing.T) {
	root, home := secretWorkdir(t)
	os.Remove(filepath.Join(root, ".hyp", "crypt.yaml"))
	if _, err := runSecret(t, "", "init", "--method", "age"); err != nil {
		t.Fatal(err)
	}
	crypt, _ := os.ReadFile(filepath.Join(root, ".hyp", "crypt.yaml"))
	if !strings.Contains(string(crypt), "method: age") || !strings.Contains(string(crypt), `- "age1`) {
		t.Fatalf("crypt.yaml not updated:\n%s", crypt)
	}
	if strings.Contains(string(crypt), "AGE-SECRET-KEY") {
		t.Fatal("private key written into crypt.yaml")
	}
	if _, err := secret.LoadSettings(root); err != nil {
		t.Fatalf("generated crypt.yaml does not parse: %v", err)
	}

	if _, err := runSecret(t, "value\n", "set", "api_key"); err != nil {
		t.Fatal(err)
	}
	if got, err := runSecret(t, "", "get", "api_key"); err != nil || got != "value\n" {
		t.Errorf("get = %q, %v", got, err)
	}

	// 只有公鑰的開發者：能 set、不能 get
	identity := filepath.Join(home, ".hypgo", "keys", "myapp.age")
	if err := os.Rename(identity, identity+".away"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "other\n", "set", "other_key"); err != nil {
		t.Errorf("set with only the public key: %v", err)
	}
	if _, err := runSecret(t, "", "get", "api_key"); err == nil {
		t.Error("get without the identity should fail")
	}
}

func TestSecretPassphraseWorkflow(t *testing.T) {
	root, _ := secretWorkdir(t)
	os.Remove(filepath.Join(root, ".hyp", "crypt.yaml"))
	if _, err := runSecret(t, "", "init", "--method", "passphrase"); err != nil {
		t.Fatal(err)
	}
	// stdin 不是終端機、也沒有環境變數：要明確失敗，不能把機密值當成密碼
	if _, err := runSecret(t, "value\n", "set", "a"); err == nil {
		t.Fatal("set without a passphrase should fail")
	}
	t.Setenv(secret.DefaultPassphraseEnv, "correct horse battery staple")
	if _, err := runSecret(t, "value\n", "set", "a"); err != nil {
		t.Fatal(err)
	}
	if got, err := runSecret(t, "", "get", "a"); err != nil || got != "value\n" {
		t.Errorf("get = %q, %v", got, err)
	}
	t.Setenv(secret.NewPassphraseEnv, "a brand new passphrase")
	if _, err := runSecret(t, "", "rotate"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secret.DefaultPassphraseEnv, "a brand new passphrase")
	if got, err := runSecret(t, "", "get", "a"); err != nil || got != "value\n" {
		t.Errorf("get after rotate = %q, %v", got, err)
	}
}

// 改 crypt.yaml 的 method 後，rotate 把整個檔案轉過去
func TestSecretRotateAfterChangingMethod(t *testing.T) {
	root, _ := secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "value\n", "set", "a"); err != nil {
		t.Fatal(err)
	}
	cryptPath := filepath.Join(root, ".hyp", "crypt.yaml")
	data, _ := os.ReadFile(cryptPath)
	os.WriteFile(cryptPath, bytes.Replace(data, []byte("\nmethod: aes-256-gcm\n"), []byte("\nmethod: chacha20-poly1305\n"), 1), 0o644)

	if _, err := runSecret(t, "", "get", "a"); err == nil || !strings.Contains(err.Error(), "rotate") {
		t.Fatalf("get before rotate: %v", err)
	}
	if _, err := runSecret(t, "", "rotate"); err != nil {
		t.Fatal(err)
	}
	if got, err := runSecret(t, "", "get", "a"); err != nil || got != "value\n" {
		t.Errorf("get after rotate = %q, %v", got, err)
	}
}

func TestSecretInitRefusesConflictingMethod(t *testing.T) {
	secretWorkdir(t)
	if _, err := runSecret(t, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSecret(t, "", "init", "--method", "age"); err == nil {
		t.Error("init --method should not silently ignore the method in crypt.yaml")
	}
}

// 所有產生的專案都帶 .hyp/crypt.yaml，且預設內容可解析、沒有多餘的 key
func TestProjectsIncludeCryptYAML(t *testing.T) {
	offlineGo(t)
	t.Chdir(t.TempDir())
	if err := runHyp(t, "new", "webapp"); err != nil {
		t.Fatal(err)
	}
	if err := runHyp(t, "api", "apiapp"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"webapp", "apiapp"} {
		data, err := os.ReadFile(filepath.Join(name, ".hyp", "crypt.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(data) != scaffold.CryptYamlTemplate {
			t.Errorf("%s: crypt.yaml differs from the template", name)
		}
		settings, err := secret.LoadSettings(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if settings.Method != secret.MethodAES {
			t.Errorf("%s: method = %s", name, settings.Method)
		}
	}
	dockerfile, _ := os.ReadFile(filepath.Join("apiapp", "Dockerfile"))
	for _, want := range []string{"key.secre[t]", ".hyp/crypt.yaml"} {
		if !strings.Contains(string(dockerfile), want) {
			t.Errorf("Dockerfile should copy %s:\n%s", want, dockerfile)
		}
	}
	if strings.Contains(string(dockerfile), "HYPGO_MASTER_KEY=") && !strings.Contains(string(dockerfile), "docker run -e") {
		t.Error("Dockerfile must not bake the key into the image")
	}
}
