// Package secret 管理 config.yaml 引用的機密值（帳號、密碼、DSN）。
//
// 機密值加密後存在 key.secret（只有密文，可進版控），config.yaml 以 ${secret:name} 引用，
// 載入設定時解密。加密方式由 .hyp/crypt.yaml 的 method 決定：
//
//   - aes-256-gcm、chacha20-poly1305：對稱式，32 bytes 金鑰來自環境變數或金鑰檔
//   - passphrase：由密碼以 Argon2id 衍生金鑰，再以 AES-256-GCM 加密
//   - age：非對稱式（X25519），公鑰即可加密，解密需要私鑰
//
// 解密金鑰不放在 key.secret 旁邊：金鑰與密文在同一處時，加密沒有任何防護效果。
package secret

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 支援的加密方式（.hyp/crypt.yaml 的 method）
const (
	MethodAES        = "aes-256-gcm"
	MethodChaCha     = "chacha20-poly1305"
	MethodPassphrase = "passphrase"
	MethodAge        = "age"
)

// 預設值
const (
	DefaultSecretFile     = "key.secret"
	DefaultKeyEnv         = "HYPGO_MASTER_KEY"
	DefaultPassphraseEnv  = "HYPGO_PASSPHRASE"
	DefaultAgeIdentityEnv = "HYPGO_AGE_IDENTITY"

	// OptionalEnv 設定時，載入設定不解密、${secret:name} 原樣保留。
	// hyp context / hyp migrate 執行專案只為取得路由與 model，不該要求 CI 持有解密金鑰
	OptionalEnv = "HYPGO_SECRET_OPTIONAL"

	// NewPassphraseEnv 為 passphrase 方式 rotate 時的新密碼（非互動環境用）
	NewPassphraseEnv = "HYPGO_NEW_PASSPHRASE"

	settingsPath = ".hyp/crypt.yaml"
)

// Settings 為 .hyp/crypt.yaml 的內容。只放設定與公鑰，不放任何機密，可進版控
type Settings struct {
	Method     string             `yaml:"method"`
	SecretFile string             `yaml:"secret_file"`
	Key        KeySettings        `yaml:"key"`
	Passphrase PassphraseSettings `yaml:"passphrase"`
	Age        AgeSettings        `yaml:"age"`

	root     string // 專案根目錄
	explicit bool   // crypt.yaml 存在：key.secret 的 method 必須與它一致
}

// KeySettings 對稱式金鑰的來源：環境變數優先，其次金鑰檔（內容為 base64）
type KeySettings struct {
	Env  string `yaml:"env"`
	File string `yaml:"file"` // 留空時為 ~/.hypgo/keys/<專案目錄名>.key
}

// PassphraseSettings 密碼的來源
type PassphraseSettings struct {
	Env string `yaml:"env"`
}

// AgeSettings age 的公鑰與私鑰來源
type AgeSettings struct {
	Recipients   []string `yaml:"recipients"`    // 公鑰（age1...），加密用
	IdentityEnv  string   `yaml:"identity_env"`  // 私鑰內容的環境變數，解密用
	IdentityFile string   `yaml:"identity_file"` // 留空時為 ~/.hypgo/keys/<專案目錄名>.age
}

// FindRoot 自 start 往上找第一個含 .hyp/ 的目錄；找不到時回傳工作目錄
func FindRoot(start string) string {
	d, err := filepath.Abs(start)
	if err != nil {
		d = start
	}
	for {
		if info, err := os.Stat(filepath.Join(d, ".hyp")); err == nil && info.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// LoadSettings 讀取 root/.hyp/crypt.yaml；檔案不存在時回傳預設值，
// 此時加密方式依 key.secret 記錄的 method（部署環境可以只帶 key.secret）。
// 未知的 key 視為錯誤：機密設定拼錯時靜默退回預設值，可能讓加密方式與預期不同
func LoadSettings(root string) (*Settings, error) {
	s := &Settings{root: root}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(settingsPath)))
	switch {
	case err == nil:
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(s); err != nil && !errors.Is(err, io.EOF) { // 只有註解時 Decode 回 EOF
			return nil, fmt.Errorf("secret: invalid %s: %w", settingsPath, err)
		}
		s.explicit = true
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("secret: read %s: %w", settingsPath, err)
	}

	s.Method = strings.ToLower(strings.TrimSpace(s.Method))
	if s.Method == "" {
		s.Method = MethodAES
	}
	if !validMethod(s.Method) {
		return nil, fmt.Errorf("secret: unknown method %q in %s (use %s, %s, %s or %s)",
			s.Method, settingsPath, MethodAES, MethodChaCha, MethodPassphrase, MethodAge)
	}
	if s.SecretFile == "" {
		s.SecretFile = DefaultSecretFile
	}
	if s.Key.Env == "" {
		s.Key.Env = DefaultKeyEnv
	}
	if s.Passphrase.Env == "" {
		s.Passphrase.Env = DefaultPassphraseEnv
	}
	if s.Age.IdentityEnv == "" {
		s.Age.IdentityEnv = DefaultAgeIdentityEnv
	}
	return s, nil
}

// Root 回傳專案根目錄
func (s *Settings) Root() string { return s.root }

// Path 回傳 key.secret 的路徑
func (s *Settings) Path() string { return s.resolve(s.SecretFile) }

// KeyFilePath 回傳對稱式金鑰檔的路徑
func (s *Settings) KeyFilePath() (string, error) {
	return s.keyPath(s.Key.File, ".key")
}

// IdentityFilePath 回傳 age 私鑰檔的路徑
func (s *Settings) IdentityFilePath() (string, error) {
	return s.keyPath(s.Age.IdentityFile, ".age")
}

// keyPath 預設放在家目錄：.hyp/ 是 AI 工具會讀取的目錄，金鑰不該留在專案裡
func (s *Settings) keyPath(configured, ext string) (string, error) {
	if configured == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("secret: locate home directory: %w", err)
		}
		return filepath.Join(home, ".hypgo", "keys", filepath.Base(s.root)+ext), nil
	}
	if rest, ok := strings.CutPrefix(configured, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("secret: locate home directory: %w", err)
		}
		return filepath.Join(home, filepath.FromSlash(rest)), nil
	}
	return s.resolve(configured), nil
}

func (s *Settings) resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.root, filepath.FromSlash(p))
}

func validMethod(m string) bool {
	switch m {
	case MethodAES, MethodChaCha, MethodPassphrase, MethodAge:
		return true
	}
	return false
}
