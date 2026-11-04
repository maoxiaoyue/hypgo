package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

const fileVersion = 1

var validName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// fileFormat 為 key.secret 的內容。依名稱排序、每項一行，git diff 只會顯示改動的項目
type fileFormat struct {
	Version int               `yaml:"version"`
	Method  string            `yaml:"method"`
	KeyID   string            `yaml:"key_id"`
	KDF     *kdfParams        `yaml:"kdf,omitempty"`
	Secrets map[string]string `yaml:"secrets"`
}

// Options 控制金鑰取得方式
type Options struct {
	// Prompt 在環境變數沒有密碼時詢問使用者；confirm 為 true 表示正在設定新密碼（需再確認一次）。
	// 為 nil 時不詢問（執行期載入設定只讀環境變數，伺服器不該卡在等待輸入）
	Prompt func(confirm bool) (string, error)

	allowMethodMismatch bool // Rotate 才允許：crypt.yaml 已改、key.secret 還是舊方式
}

// Store 為開啟中的 key.secret
type Store struct {
	settings *Settings
	opts     Options
	file     fileFormat
	exists   bool
	cipher   valueCipher // 已驗證金鑰相符的加解密器
	writable bool        // cipher 可加密（age 只有私鑰時不行）
	readable bool        // cipher 可解密（age 只有公鑰時不行）
}

// Open 讀取 key.secret（不存在時視為空），此時還不需要金鑰：list 與 age 的 set 都用不到私鑰
func Open(settings *Settings, opts Options) (*Store, error) {
	st := &Store{settings: settings, opts: opts}
	data, err := os.ReadFile(settings.Path())
	if errors.Is(err, os.ErrNotExist) {
		st.file = fileFormat{Version: fileVersion, Method: settings.Method, Secrets: map[string]string{}}
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secret: read %s: %w", settings.SecretFile, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&st.file); err != nil {
		return nil, fmt.Errorf("secret: invalid %s: %w", settings.SecretFile, err)
	}
	if st.file.Version != fileVersion {
		return nil, fmt.Errorf("secret: %s has unsupported version %d", settings.SecretFile, st.file.Version)
	}
	if !validMethod(st.file.Method) {
		return nil, fmt.Errorf("secret: %s has unknown method %q", settings.SecretFile, st.file.Method)
	}
	if settings.explicit && st.file.Method != settings.Method && !opts.allowMethodMismatch {
		return nil, fmt.Errorf("secret: %s uses %s but .hyp/crypt.yaml says %s: run hyp secret rotate",
			settings.SecretFile, st.file.Method, settings.Method)
	}
	if st.file.Secrets == nil {
		st.file.Secrets = map[string]string{}
	}
	st.exists = true
	return st, nil
}

// Method 回傳 key.secret 使用的加密方式
func (st *Store) Method() string { return st.file.Method }

// Names 回傳已儲存的名稱（已排序）
func (st *Store) Names() []string {
	names := make([]string, 0, len(st.file.Secrets))
	for n := range st.file.Secrets {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Has 回報名稱是否存在
func (st *Store) Has(name string) bool {
	_, ok := st.file.Secrets[name]
	return ok
}

// Get 解密並回傳機密值。錯誤訊息只含名稱
func (st *Store) Get(name string) (string, error) {
	sealed, ok := st.file.Secrets[name]
	if !ok {
		return "", fmt.Errorf("secret: %q not found in %s", name, st.settings.SecretFile)
	}
	if err := st.prepare(true, false); err != nil {
		return "", err
	}
	pt, err := st.cipher.open(name, sealed)
	if err != nil {
		return "", fmt.Errorf("secret: decrypt %q: %w", name, err)
	}
	return string(pt), nil
}

// Set 加密並寫入機密值
func (st *Store) Set(name, value string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("secret: invalid name %q (letters, digits, _ . - only)", name)
	}
	if err := st.prepare(false, true); err != nil {
		return err
	}
	sealed, err := st.cipher.seal(name, []byte(value))
	if err != nil {
		return fmt.Errorf("secret: encrypt %q: %w", name, err)
	}
	st.file.Secrets[name] = sealed
	return st.save()
}

// Delete 移除機密值；不需要金鑰
func (st *Store) Delete(name string) error {
	if !st.Has(name) {
		return fmt.Errorf("secret: %q not found in %s", name, st.settings.SecretFile)
	}
	delete(st.file.Secrets, name)
	return st.save()
}

// prepare 取得金鑰並確認它與 key.secret 相符。金鑰不符時立刻回錯：
// 否則用錯金鑰 set 會讓同一個檔案混雜兩把金鑰的項目，之後永遠有一部分解不開
func (st *Store) prepare(needRead, needWrite bool) error {
	if st.cipher != nil && (!needRead || st.readable) && (!needWrite || st.writable) {
		return nil
	}
	switch st.file.Method {
	case MethodAES, MethodChaCha:
		key, err := st.settings.loadSymmetricKey()
		if err != nil {
			return err
		}
		return st.useKey(key)
	case MethodPassphrase:
		return st.preparePassphrase()
	default:
		return st.prepareAge(needRead, needWrite)
	}
}

func (st *Store) useKey(key []byte) error {
	id := shortID(key)
	if st.file.KeyID != "" && st.file.KeyID != id {
		return fmt.Errorf("secret: key does not match %s (file was encrypted with key %s, this key is %s)",
			st.settings.SecretFile, st.file.KeyID, id)
	}
	c, err := newAEADCipher(aeadFor(st.file.Method), key)
	if err != nil {
		return err
	}
	st.file.KeyID, st.cipher, st.readable, st.writable = id, c, true, true
	return nil
}

// aeadFor passphrase 衍生的金鑰固定使用 AES-256-GCM
func aeadFor(method string) string {
	if method == MethodChaCha {
		return MethodChaCha
	}
	return MethodAES
}

func (st *Store) preparePassphrase() error {
	creating := st.file.KDF == nil
	pass, err := st.passphrase(creating)
	if err != nil {
		return err
	}
	if creating {
		if len(pass) < minPassphrase {
			return fmt.Errorf("secret: passphrase must be at least %d characters", minPassphrase)
		}
		if st.file.KDF, err = newKDF(); err != nil {
			return err
		}
	}
	key, err := st.file.KDF.derive(pass)
	if err != nil {
		return err
	}
	if err := st.useKey(key); err != nil {
		if creating {
			st.file.KDF = nil
		}
		return errors.New("secret: wrong passphrase for " + st.settings.SecretFile)
	}
	return nil
}

func (st *Store) passphrase(confirm bool) (string, error) {
	if v := os.Getenv(st.settings.Passphrase.Env); v != "" {
		return v, nil
	}
	if st.opts.Prompt != nil {
		return st.opts.Prompt(confirm)
	}
	return "", fmt.Errorf("secret: no passphrase: set $%s", st.settings.Passphrase.Env)
}

// prepareAge 只取得這次操作需要的部分：開發者與 CI 只有公鑰也能 set
func (st *Store) prepareAge(needRead, needWrite bool) error {
	c, _ := st.cipher.(*ageCipher)
	if c == nil {
		c = &ageCipher{}
	}
	if needWrite && !st.writable {
		recipients, err := st.settings.parseRecipients()
		if err != nil {
			return err
		}
		id := st.settings.recipientsID()
		if st.file.KeyID != "" && st.file.KeyID != id {
			return errors.New("secret: age.recipients changed since " + st.settings.SecretFile + " was written: run hyp secret rotate")
		}
		st.file.KeyID = id
		c.recipients, st.writable = recipients, true
	}
	if needRead && !st.readable {
		identities, err := st.settings.loadIdentities()
		if err != nil {
			return err
		}
		c.identities, st.readable = identities, true
	}
	st.cipher = c
	return nil
}

// save 寫入暫存檔再 rename：寫到一半失敗時不會毀掉既有的 key.secret
func (st *Store) save() error {
	data, err := yaml.Marshal(&st.file)
	if err != nil {
		return err
	}
	path := st.settings.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("secret: create directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".key.secret-*")
	if err != nil {
		return fmt.Errorf("secret: write %s: %w", st.settings.SecretFile, err)
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o640)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		return fmt.Errorf("secret: write %s: %w", st.settings.SecretFile, werr)
	}
	st.exists = true
	return nil
}

// RotateOptions 為 rotate 的新金鑰
type RotateOptions struct {
	NewKey        []byte // 對稱式：新金鑰；方式不變時必填
	NewPassphrase string // passphrase：新密碼；留空時依序讀 NewPassphraseEnv、Prompt
}

// Rotate 以 .hyp/crypt.yaml 目前的設定重新加密 key.secret 的全部項目，回傳項目數。
// 舊金鑰來自目前的金鑰來源；全部解密成功、新密文也驗證可解開後才寫檔，任何一步失敗都不動原檔
func Rotate(settings *Settings, opts Options, ropts RotateOptions) (int, error) {
	opts.allowMethodMismatch = true
	old, err := Open(settings, opts)
	if err != nil {
		return 0, err
	}
	if !old.exists {
		return 0, fmt.Errorf("secret: %s does not exist", settings.SecretFile)
	}
	plain := make(map[string]string, len(old.file.Secrets))
	for _, name := range old.Names() {
		if plain[name], err = old.Get(name); err != nil {
			return 0, err
		}
	}

	next := &Store{settings: settings, opts: opts, file: fileFormat{
		Version: fileVersion, Method: settings.Method, Secrets: map[string]string{},
	}}
	same := old.file.Method == settings.Method
	switch settings.Method {
	case MethodAES, MethodChaCha:
		key := ropts.NewKey
		if key == nil {
			if same {
				return 0, errors.New("secret: rotating with the same method needs a new key (hyp secret keygen, then --new-key-file)")
			}
			if key, err = settings.loadSymmetricKey(); err != nil {
				return 0, err
			}
		}
		if err := next.useKey(key); err != nil {
			return 0, err
		}
		if same && next.file.KeyID == old.file.KeyID {
			return 0, errors.New("secret: the new key is the same as the current key")
		}
	case MethodPassphrase:
		pass := ropts.NewPassphrase
		if pass == "" && same {
			if pass = os.Getenv(NewPassphraseEnv); pass == "" && opts.Prompt != nil {
				if pass, err = opts.Prompt(true); err != nil {
					return 0, err
				}
			}
			if pass == "" {
				return 0, fmt.Errorf("secret: rotating with the same method needs a new passphrase: set $%s", NewPassphraseEnv)
			}
		} else if pass == "" {
			if pass, err = next.passphrase(true); err != nil {
				return 0, err
			}
		}
		if len(pass) < minPassphrase {
			return 0, fmt.Errorf("secret: passphrase must be at least %d characters", minPassphrase)
		}
		if next.file.KDF, err = newKDF(); err != nil {
			return 0, err
		}
		key, err := next.file.KDF.derive(pass)
		if err != nil {
			return 0, err
		}
		if err := next.useKey(key); err != nil {
			return 0, err
		}
	case MethodAge:
		if same && old.file.KeyID == settings.recipientsID() {
			return 0, errors.New("secret: age.recipients unchanged: update .hyp/crypt.yaml first")
		}
		if err := next.prepareAge(false, true); err != nil {
			return 0, err
		}
	}

	for name, value := range plain {
		sealed, err := next.cipher.seal(name, []byte(value))
		if err != nil {
			return 0, fmt.Errorf("secret: encrypt %q: %w", name, err)
		}
		next.file.Secrets[name] = sealed
		// age 的新私鑰可能不在這台機器上，無法驗證解開；對稱式一律驗證
		if settings.Method != MethodAge {
			pt, err := next.cipher.open(name, sealed)
			if err != nil || string(pt) != value {
				return 0, fmt.Errorf("secret: verify %q failed, %s not changed", name, settings.SecretFile)
			}
		}
	}
	if err := next.save(); err != nil {
		return 0, err
	}
	return len(plain), nil
}
