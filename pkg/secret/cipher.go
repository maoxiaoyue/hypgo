package secret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	keySize       = 32
	saltSize      = 16
	minPassphrase = 12

	// Argon2id 參數（RFC 9106 的低記憶體建議值）
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4

	// key.secret 可能在版控中被改動：參數設上限，避免竄改後讓啟動耗盡記憶體或 CPU
	maxArgonTime    = 16
	maxArgonMemory  = 1024 * 1024 // KiB
	maxArgonThreads = 64
)

// kdfParams 記錄於 key.secret，日後調整預設值不影響既有檔案
type kdfParams struct {
	Name    string `yaml:"name"`
	Salt    string `yaml:"salt"`
	Time    uint32 `yaml:"time"`
	Memory  uint32 `yaml:"memory_kib"`
	Threads uint8  `yaml:"threads"`
}

// valueCipher 加解密單一機密值。name 綁進密文，防止有人把兩筆密文對調（例如密碼與帳號互換）
type valueCipher interface {
	seal(name string, plaintext []byte) (string, error)
	open(name, sealed string) ([]byte, error)
}

// aeadCipher 對稱式 AEAD（AES-256-GCM、ChaCha20-Poly1305）
type aeadCipher struct {
	method string
	aead   cipher.AEAD
}

func newAEADCipher(method string, key []byte) (*aeadCipher, error) {
	var (
		aead cipher.AEAD
		err  error
	)
	if method == MethodChaCha {
		aead, err = chacha20poly1305.New(key)
	} else {
		var block cipher.Block
		if block, err = aes.NewCipher(key); err == nil {
			aead, err = cipher.NewGCM(block)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("secret: init %s: %w", method, err)
	}
	return &aeadCipher{method: method, aead: aead}, nil
}

// aad 把 method 與名稱綁進驗證：改動 key.secret 的 method 或對調項目都會解密失敗
func (c *aeadCipher) aad(name string) []byte {
	return []byte("hypgo-secret/v1\x00" + c.method + "\x00" + name)
}

func (c *aeadCipher) seal(name string, plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, plaintext, c.aad(name))), nil
}

func (c *aeadCipher) open(name, sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, errors.New("not valid base64")
	}
	if len(raw) < c.aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	pt, err := c.aead.Open(nil, nonce, ct, c.aad(name))
	if err != nil {
		return nil, errors.New("wrong key or tampered data")
	}
	return pt, nil
}

// ageCipher 非對稱式：recipients 加密、identities 解密，兩者可以只有其一。
// age 沒有 AAD，名稱放在明文開頭達到相同效果
type ageCipher struct {
	recipients []age.Recipient
	identities []age.Identity
}

func (c *ageCipher) seal(name string, plaintext []byte) (string, error) {
	if len(c.recipients) == 0 {
		return "", errors.New("no age recipients: add them to age.recipients in .hyp/crypt.yaml")
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, c.recipients...)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(w, name+"\x00"); err != nil {
		return "", err
	}
	if _, err := w.Write(plaintext); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (c *ageCipher) open(name, sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, errors.New("not valid base64")
	}
	r, err := age.Decrypt(bytes.NewReader(raw), c.identities...)
	if err != nil {
		return nil, errors.New("wrong identity or tampered data")
	}
	pt, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.New("wrong identity or tampered data")
	}
	prefix := []byte(name + "\x00")
	if !bytes.HasPrefix(pt, prefix) {
		return nil, errors.New("entry was encrypted under a different name")
	}
	return pt[len(prefix):], nil
}

// GenerateKey 產生對稱式金鑰（base64 編碼）
func GenerateKey() (string, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// GenerateAgeIdentity 產生 age 金鑰對，回傳私鑰與公鑰字串
func GenerateAgeIdentity() (identity, recipient string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// WriteKeyFile 以 0600 建立金鑰檔；已存在時回傳錯誤，絕不覆寫（覆寫等於永久失去解密能力）
func WriteKeyFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secret: create key directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("secret: create key file: %w", err)
	}
	_, werr := f.WriteString(content + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return fmt.Errorf("secret: write key file: %w", werr)
	}
	return nil
}

// RecipientFromIdentity 由私鑰內容（可含註解行）取得對應的公鑰
func RecipientFromIdentity(identity string) (string, error) {
	ids, err := age.ParseIdentities(strings.NewReader(identity))
	if err != nil {
		return "", errors.New("secret: invalid age identity")
	}
	for _, id := range ids {
		if x, ok := id.(*age.X25519Identity); ok {
			return x.Recipient().String(), nil
		}
	}
	return "", errors.New("secret: no X25519 identity found")
}

// DecodeKey 解析 base64 編碼的 32 bytes 金鑰。錯誤訊息不含內容
func DecodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, errors.New("key is not valid base64")
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", keySize, len(key))
	}
	return key, nil
}

// loadSymmetricKey 依序從環境變數、金鑰檔取得金鑰
func (s *Settings) loadSymmetricKey() ([]byte, error) {
	if v := os.Getenv(s.Key.Env); v != "" {
		key, err := DecodeKey(v)
		if err != nil {
			return nil, fmt.Errorf("secret: $%s: %w", s.Key.Env, err)
		}
		return key, nil
	}
	path, err := s.KeyFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secret: no key found: set $%s or create %s (hyp secret init)", s.Key.Env, path)
	}
	if err != nil {
		return nil, fmt.Errorf("secret: read key file: %w", err)
	}
	key, err := DecodeKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("secret: %s: %w", path, err)
	}
	return key, nil
}

// loadIdentities 依序從環境變數、私鑰檔取得 age 私鑰
func (s *Settings) loadIdentities() ([]age.Identity, error) {
	content := os.Getenv(s.Age.IdentityEnv)
	source := "$" + s.Age.IdentityEnv
	if content == "" {
		path, err := s.IdentityFilePath()
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("secret: no age identity found: set $%s or create %s (hyp secret init)", s.Age.IdentityEnv, path)
		}
		if err != nil {
			return nil, fmt.Errorf("secret: read identity file: %w", err)
		}
		content, source = string(data), path
	}
	ids, err := age.ParseIdentities(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("secret: invalid age identity in %s", source)
	}
	return ids, nil
}

// parseRecipients 解析 crypt.yaml 的公鑰
func (s *Settings) parseRecipients() ([]age.Recipient, error) {
	if len(s.Age.Recipients) == 0 {
		return nil, errors.New("secret: no age recipients: add them to age.recipients in .hyp/crypt.yaml (hyp secret init)")
	}
	out, err := age.ParseRecipients(strings.NewReader(strings.Join(s.Age.Recipients, "\n")))
	if err != nil {
		return nil, fmt.Errorf("secret: invalid age recipient in .hyp/crypt.yaml: %w", err)
	}
	return out, nil
}

// recipientsID 公鑰組合的識別碼：組合改變時 key.secret 必須 rotate，否則部分成員解不開部分項目
func (s *Settings) recipientsID() string {
	sorted := append([]string(nil), s.Age.Recipients...)
	sort.Strings(sorted)
	return shortID([]byte(strings.Join(sorted, "\n")))
}

// shortID 由金鑰算出的識別碼：解密前就能明確回報「金鑰不對」，而不是籠統的解密失敗
func shortID(key []byte) string {
	sum := sha256.Sum256(append([]byte("hypgo-secret-key-id\x00"), key...))
	return hex.EncodeToString(sum[:8])
}

func newKDF() (*kdfParams, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return &kdfParams{
		Name:    "argon2id",
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    argonTime,
		Memory:  argonMemory,
		Threads: argonThreads,
	}, nil
}

func (k *kdfParams) derive(passphrase string) ([]byte, error) {
	if k.Name != "argon2id" {
		return nil, fmt.Errorf("secret: unsupported kdf %q", k.Name)
	}
	if k.Time < 1 || k.Time > maxArgonTime || k.Memory < 8*1024 || k.Memory > maxArgonMemory ||
		k.Threads < 1 || k.Threads > maxArgonThreads {
		return nil, errors.New("secret: kdf parameters out of range")
	}
	salt, err := base64.StdEncoding.DecodeString(k.Salt)
	if err != nil || len(salt) < saltSize {
		return nil, errors.New("secret: invalid kdf salt")
	}
	return argon2.IDKey([]byte(passphrase), salt, k.Time, k.Memory, k.Threads, keySize), nil
}
