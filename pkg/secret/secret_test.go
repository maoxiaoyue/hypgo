package secret

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testPlain = "s3cr3t-P@ss:word/with$pecial"

// newProject 建立暫存專案並寫入 crypt.yaml，金鑰與私鑰都透過環境變數提供（不碰使用者的家目錄）
func newProject(t *testing.T, method string) *Settings {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	if err := os.MkdirAll(filepath.Join(root, ".hyp"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "method: " + method + "\n"
	switch method {
	case MethodAES, MethodChaCha:
		key, err := GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(DefaultKeyEnv, key)
	case MethodPassphrase:
		t.Setenv(DefaultPassphraseEnv, "correct horse battery staple")
	case MethodAge:
		identity, recipient, err := GenerateAgeIdentity()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(DefaultAgeIdentityEnv, identity)
		content += "age:\n  recipients:\n    - " + recipient + "\n"
	}
	writeCrypt(t, root, content)
	return mustSettings(t, root)
}

func writeCrypt(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".hyp", "crypt.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSettings(t *testing.T, root string) *Settings {
	t.Helper()
	s, err := LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustOpen(t *testing.T, s *Settings) *Store {
	t.Helper()
	st, err := Open(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func readFile(t *testing.T, s *Settings) fileFormat {
	t.Helper()
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var f fileFormat
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func writeFile(t *testing.T, s *Settings, f fileFormat) {
	t.Helper()
	data, err := yaml.Marshal(&f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(), data, 0o640); err != nil {
		t.Fatal(err)
	}
}

var allMethods = []string{MethodAES, MethodChaCha, MethodPassphrase, MethodAge}

func TestRoundTripAllMethods(t *testing.T) {
	for _, method := range allMethods {
		t.Run(method, func(t *testing.T) {
			s := newProject(t, method)
			if err := mustOpen(t, s).Set("db_pass", testPlain); err != nil {
				t.Fatal(err)
			}
			if err := mustOpen(t, s).Set("db_user", "app"); err != nil {
				t.Fatal(err)
			}

			raw, _ := os.ReadFile(s.Path())
			if strings.Contains(string(raw), testPlain) {
				t.Fatal("key.secret contains the plaintext")
			}

			st := mustOpen(t, s)
			if st.Method() != method {
				t.Errorf("Method = %s", st.Method())
			}
			if got, err := st.Get("db_pass"); err != nil || got != testPlain {
				t.Errorf("Get = %q, %v", got, err)
			}
			if names := st.Names(); len(names) != 2 || names[0] != "db_pass" {
				t.Errorf("Names = %v", names)
			}
			if err := st.Delete("db_user"); err != nil {
				t.Fatal(err)
			}
			if mustOpen(t, s).Has("db_user") {
				t.Error("Delete did not persist")
			}
		})
	}
}

// 相同明文每次加密的結果都要不同（nonce 重複會直接破壞 AES-GCM 的安全性）
func TestNonceIsRandom(t *testing.T) {
	for _, method := range allMethods {
		t.Run(method, func(t *testing.T) {
			s := newProject(t, method)
			st := mustOpen(t, s)
			if err := st.Set("a", testPlain); err != nil {
				t.Fatal(err)
			}
			first := st.file.Secrets["a"]
			if err := st.Set("a", testPlain); err != nil {
				t.Fatal(err)
			}
			if st.file.Secrets["a"] == first {
				t.Error("same plaintext produced identical ciphertext")
			}
		})
	}
}

func TestTamperAndSwapAreRejected(t *testing.T) {
	for _, method := range allMethods {
		t.Run(method, func(t *testing.T) {
			s := newProject(t, method)
			st := mustOpen(t, s)
			if err := st.Set("a", "value-a"); err != nil {
				t.Fatal(err)
			}
			if err := st.Set("b", "value-b"); err != nil {
				t.Fatal(err)
			}

			// 對調兩筆密文：有 AAD / 名稱綁定時必須解密失敗，而不是把帳號當密碼回傳
			f := readFile(t, s)
			swapped := f
			swapped.Secrets = map[string]string{"a": f.Secrets["b"], "b": f.Secrets["a"]}
			writeFile(t, s, swapped)
			if got, err := mustOpen(t, s).Get("a"); err == nil {
				t.Errorf("swapped entries decrypted to %q", got)
			}

			// 翻轉密文最後一個 byte
			raw, err := base64.StdEncoding.DecodeString(f.Secrets["a"])
			if err != nil {
				t.Fatal(err)
			}
			raw[len(raw)-1] ^= 0xff
			tampered := f
			tampered.Secrets = map[string]string{"a": base64.StdEncoding.EncodeToString(raw)}
			writeFile(t, s, tampered)
			if got, err := mustOpen(t, s).Get("a"); err == nil {
				t.Errorf("tampered entry decrypted to %q", got)
			}
		})
	}
}

// 把 key.secret 的 method 改成另一種 AEAD：AAD 綁了 method，同一把金鑰也解不開
func TestMethodFieldIsAuthenticated(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	f := readFile(t, s)
	f.Method = MethodChaCha
	writeFile(t, s, f)
	writeCrypt(t, s.Root(), "method: "+MethodChaCha+"\n")
	if got, err := mustOpen(t, mustSettings(t, s.Root())).Get("a"); err == nil {
		t.Errorf("decrypted to %q after changing method", got)
	}
}

func TestWrongKeyFailsWithoutLeaking(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	other, _ := GenerateKey()
	t.Setenv(DefaultKeyEnv, other)

	st := mustOpen(t, s)
	_, err := st.Get("a")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Get with wrong key: %v", err)
	}
	if strings.Contains(err.Error(), other) || strings.Contains(err.Error(), testPlain) {
		t.Errorf("error leaks secret material: %v", err)
	}
	// 用錯金鑰 set 會讓檔案混雜兩把金鑰的項目，必須拒絕
	if err := st.Set("b", "x"); err == nil {
		t.Error("Set with a different key should fail")
	}
	if mustOpen(t, s).Has("b") {
		t.Error("a value encrypted with the wrong key was stored")
	}
}

func TestWrongPassphrase(t *testing.T) {
	s := newProject(t, MethodPassphrase)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DefaultPassphraseEnv, "this is not the passphrase")
	if _, err := mustOpen(t, s).Get("a"); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("Get: %v", err)
	}
}

func TestPassphraseTooShortOnCreate(t *testing.T) {
	s := newProject(t, MethodPassphrase)
	t.Setenv(DefaultPassphraseEnv, "short")
	if err := mustOpen(t, s).Set("a", "x"); err == nil {
		t.Error("short passphrase accepted")
	}
	if _, err := os.Stat(s.Path()); err == nil {
		t.Error("key.secret written despite the error")
	}
}

// 竄改 KDF 參數不能讓啟動耗盡記憶體
func TestKDFParamsAreBounded(t *testing.T) {
	s := newProject(t, MethodPassphrase)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	f := readFile(t, s)
	f.KDF.Memory = 1 << 31
	writeFile(t, s, f)
	if _, err := mustOpen(t, s).Get("a"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("Get: %v", err)
	}
}

// age 的重點：只有公鑰的人可以 set，但解不開
func TestAgePublicKeyCanEncryptButNotDecrypt(t *testing.T) {
	s := newProject(t, MethodAge)
	identity := os.Getenv(DefaultAgeIdentityEnv)
	t.Setenv(DefaultAgeIdentityEnv, "")

	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatalf("Set with public key only: %v", err)
	}
	if _, err := mustOpen(t, s).Get("a"); err == nil {
		t.Fatal("Get without identity should fail")
	}

	t.Setenv(DefaultAgeIdentityEnv, identity)
	if got, err := mustOpen(t, s).Get("a"); err != nil || got != testPlain {
		t.Errorf("Get with identity = %q, %v", got, err)
	}
}

func TestAgeRecipientsChangeRequiresRotate(t *testing.T) {
	s := newProject(t, MethodAge)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	_, other, _ := GenerateAgeIdentity()
	writeCrypt(t, s.Root(), "method: age\nage:\n  recipients:\n    - "+other+"\n")
	if err := mustOpen(t, mustSettings(t, s.Root())).Set("b", "x"); err == nil || !strings.Contains(err.Error(), "rotate") {
		t.Errorf("Set: %v", err)
	}
}

func TestMethodMismatchNeedsRotate(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	writeCrypt(t, s.Root(), "method: "+MethodChaCha+"\n")
	if _, err := Open(mustSettings(t, s.Root()), Options{}); err == nil || !strings.Contains(err.Error(), "rotate") {
		t.Errorf("Open: %v", err)
	}
}

func TestRotateChangesMethod(t *testing.T) {
	s := newProject(t, MethodAES)
	st := mustOpen(t, s)
	if err := st.Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("b", "other"); err != nil {
		t.Fatal(err)
	}

	t.Setenv(DefaultPassphraseEnv, "a long enough passphrase")
	identity, recipient, _ := GenerateAgeIdentity()
	t.Setenv(DefaultAgeIdentityEnv, identity)
	for _, content := range []string{
		"method: " + MethodChaCha + "\n",
		"method: " + MethodPassphrase + "\n",
		"method: age\nage:\n  recipients:\n    - " + recipient + "\n",
		"method: " + MethodAES + "\n",
	} {
		writeCrypt(t, s.Root(), content)
		next := mustSettings(t, s.Root())
		if n, err := Rotate(next, Options{}, RotateOptions{}); err != nil || n != 2 {
			t.Fatalf("Rotate to %s: n=%d err=%v", next.Method, n, err)
		}
		got := mustOpen(t, next)
		if got.Method() != next.Method {
			t.Errorf("Method = %s, want %s", got.Method(), next.Method)
		}
		if v, err := got.Get("a"); err != nil || v != testPlain {
			t.Errorf("after rotate to %s: %q, %v", next.Method, v, err)
		}
	}
}

func TestRotateNewSymmetricKey(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	if _, err := Rotate(s, Options{}, RotateOptions{}); err == nil {
		t.Error("same method without a new key should fail")
	}
	current, _ := DecodeKey(os.Getenv(DefaultKeyEnv))
	if _, err := Rotate(s, Options{}, RotateOptions{NewKey: current}); err == nil {
		t.Error("rotating to the same key should fail")
	}

	encoded, _ := GenerateKey()
	newKey, _ := DecodeKey(encoded)
	if _, err := Rotate(s, Options{}, RotateOptions{NewKey: newKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := mustOpen(t, s).Get("a"); err == nil {
		t.Error("the old key still decrypts after rotation")
	}
	t.Setenv(DefaultKeyEnv, encoded)
	if v, err := mustOpen(t, s).Get("a"); err != nil || v != testPlain {
		t.Errorf("new key: %q, %v", v, err)
	}
}

func TestRotateNewPassphrase(t *testing.T) {
	s := newProject(t, MethodPassphrase)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	if _, err := Rotate(s, Options{}, RotateOptions{}); err == nil {
		t.Error("same method without a new passphrase should fail")
	}
	t.Setenv(NewPassphraseEnv, "a brand new passphrase")
	if _, err := Rotate(s, Options{}, RotateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DefaultPassphraseEnv, "a brand new passphrase")
	if v, err := mustOpen(t, s).Get("a"); err != nil || v != testPlain {
		t.Errorf("new passphrase: %q, %v", v, err)
	}
}

// 任何一筆解不開時都不能動原檔：否則部分項目會永久遺失
func TestRotateFailureLeavesFileUntouched(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path())

	wrong, _ := GenerateKey()
	t.Setenv(DefaultKeyEnv, wrong)
	newKey, _ := DecodeKey(wrong)
	if _, err := Rotate(s, Options{}, RotateOptions{NewKey: newKey}); err == nil {
		t.Fatal("Rotate with the wrong current key should fail")
	}
	after, _ := os.ReadFile(s.Path())
	if string(before) != string(after) {
		t.Error("key.secret changed although rotate failed")
	}
}

func TestSettingsRejectsTyposAndUnknownMethod(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".hyp"), 0o755)

	writeCrypt(t, root, "mehtod: age\n")
	if _, err := LoadSettings(root); err == nil {
		t.Error("typo in crypt.yaml should fail, not silently use the default method")
	}
	writeCrypt(t, root, "method: rot13\n")
	if _, err := LoadSettings(root); err == nil {
		t.Error("unknown method should fail")
	}
	writeCrypt(t, root, "# only comments\n")
	if s, err := LoadSettings(root); err != nil || s.Method != MethodAES {
		t.Errorf("comments only: %+v, %v", s, err)
	}
}

// 部署環境可以沒有 crypt.yaml：方式依 key.secret 記錄，金鑰來源用預設值
func TestWorksWithoutCryptYAML(t *testing.T) {
	s := newProject(t, MethodChaCha)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(s.Root(), ".hyp", "crypt.yaml"))
	got := mustOpen(t, mustSettings(t, s.Root()))
	if got.Method() != MethodChaCha {
		t.Errorf("Method = %s", got.Method())
	}
	if v, err := got.Get("a"); err != nil || v != testPlain {
		t.Errorf("Get: %q, %v", v, err)
	}
}

func TestKeyFile(t *testing.T) {
	s := newProject(t, MethodAES)
	t.Setenv(DefaultKeyEnv, "")
	path, err := s.KeyFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(s.Root(), path); err != nil || !strings.HasPrefix(rel, ".hypgo") {
		t.Errorf("default key path should be under home: %s", path)
	}
	if err := mustOpen(t, s).Set("a", "x"); err == nil || !strings.Contains(err.Error(), "no key found") {
		t.Fatalf("missing key: %v", err)
	}

	key, _ := GenerateKey()
	if err := WriteKeyFile(path, key); err != nil {
		t.Fatal(err)
	}
	if err := WriteKeyFile(path, "other"); err == nil {
		t.Error("WriteKeyFile must never overwrite an existing key")
	}
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	if v, err := mustOpen(t, s).Get("a"); err != nil || v != testPlain {
		t.Errorf("Get: %q, %v", v, err)
	}
}

func TestInvalidName(t *testing.T) {
	s := newProject(t, MethodAES)
	for _, name := range []string{"", "a b", "a}b", "a\nb", "../x"} {
		if err := mustOpen(t, s).Set(name, "x"); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
}

type expandInner struct {
	Password string
}

type expandCfg struct {
	DSN      string
	Plain    string
	Inner    expandInner
	Ptr      *expandInner
	List     []expandInner
	Tags     []string
	Map      map[string]string
	Any      map[string]interface{}
	Count    int
	internal string
}

func TestExpandConfig(t *testing.T) {
	s := newProject(t, MethodAES)
	st := mustOpen(t, s)
	if err := st.Set("db_pass", testPlain); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("user", "app"); err != nil {
		t.Fatal(err)
	}

	ref := "${secret:db_pass}"
	cfg := &expandCfg{
		DSN:      "${secret:user}:" + ref + "@tcp(db:3306)/app",
		Plain:    "no reference $HOME ${OTHER}",
		Inner:    expandInner{Password: ref},
		Ptr:      &expandInner{Password: ref},
		List:     []expandInner{{Password: ref}},
		Tags:     []string{ref},
		Map:      map[string]string{"k": ref},
		Any:      map[string]interface{}{"k": ref, "n": 1, "nested": map[string]interface{}{"k": ref}},
		internal: ref,
	}
	configFile := filepath.Join(s.Root(), "config", "config.yaml")
	if err := ExpandConfig(cfg, configFile); err != nil {
		t.Fatal(err)
	}
	if want := "app:" + testPlain + "@tcp(db:3306)/app"; cfg.DSN != want {
		t.Errorf("DSN = %q", cfg.DSN)
	}
	if cfg.Plain != "no reference $HOME ${OTHER}" {
		t.Errorf("Plain was modified: %q", cfg.Plain)
	}
	if cfg.Inner.Password != testPlain || cfg.Ptr.Password != testPlain || cfg.List[0].Password != testPlain ||
		cfg.Tags[0] != testPlain || cfg.Map["k"] != testPlain || cfg.Any["k"] != testPlain {
		t.Errorf("not all references expanded: %+v", cfg)
	}
	if cfg.Any["nested"].(map[string]interface{})["k"] != testPlain || cfg.Any["n"] != 1 {
		t.Errorf("nested map: %+v", cfg.Any)
	}
}

// 解密後的值再含 ${secret:x} 不能被展開第二次
func TestExpandIsNotRecursive(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", "${secret:a}"); err != nil {
		t.Fatal(err)
	}
	cfg := &expandCfg{Plain: "${secret:a}"}
	if err := ExpandConfig(cfg, filepath.Join(s.Root(), "c.yaml")); err != nil {
		t.Fatal(err)
	}
	if cfg.Plain != "${secret:a}" {
		t.Errorf("Plain = %q", cfg.Plain)
	}
}

func TestExpandErrorNamesFieldNotValue(t *testing.T) {
	s := newProject(t, MethodAES)
	if err := mustOpen(t, s).Set("a", testPlain); err != nil {
		t.Fatal(err)
	}
	cfg := &expandCfg{Inner: expandInner{Password: "${secret:a}"}, DSN: "x:${secret:missing}"}
	err := ExpandConfig(cfg, filepath.Join(s.Root(), "c.yaml"))
	if err == nil || !strings.Contains(err.Error(), "DSN") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testPlain) {
		t.Errorf("error leaks the value: %v", err)
	}
}

// 沒有引用時不需要金鑰、key.secret 也可以不存在：沒用這個功能的專案不受任何影響
func TestExpandWithoutReferencesNeedsNoKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv(DefaultKeyEnv, "")
	cfg := &expandCfg{DSN: "plain"}
	if err := ExpandConfig(cfg, filepath.Join(root, "c.yaml")); err != nil {
		t.Fatal(err)
	}
}

// hyp context / hyp migrate 執行專案時不解密，CI 不需要金鑰
func TestExpandOptionalKeepsPlaceholder(t *testing.T) {
	root := t.TempDir()
	t.Setenv(OptionalEnv, "1")
	cfg := &expandCfg{DSN: "${secret:a}"}
	if err := ExpandConfig(cfg, filepath.Join(root, "c.yaml")); err != nil {
		t.Fatal(err)
	}
	if cfg.DSN != "${secret:a}" {
		t.Errorf("DSN = %q", cfg.DSN)
	}
}
