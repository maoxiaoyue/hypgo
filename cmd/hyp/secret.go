package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/maoxiaoyue/hypgo/pkg/secret"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// maxSecretValue 限制從 stdin 讀入的機密值大小
const maxSecretValue = 1 << 20

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Encrypt credentials into key.secret, referenced as ${secret:name} in config.yaml",
	Long: `Store credentials encrypted in key.secret (ciphertext only, safe to commit) and
reference them from config.yaml, e.g.:

  database:
    dsn: "app:${secret:db_pass}@tcp(db:3306)/app"

The encryption method is set in .hyp/crypt.yaml:
  aes-256-gcm, chacha20-poly1305   32-byte key from an env var or a key file
  passphrase                       key derived from a passphrase with Argon2id
  age                              public key encrypts, private key decrypts

Examples:
  hyp secret init                  # create crypt.yaml and generate the key
  hyp secret set db_pass           # value is read from the terminal or stdin
  hyp secret list
  hyp secret rotate --new-key-file new.key
  hyp secret --restore             # write the plaintext back into config/*.yaml

The key file (.hyp/secret.key, or .hyp/secret.age) must never be committed:
hyp secret init adds it to .gitignore, and every hyp secret command refuses to
run while git tracks it.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if restore, _ := cmd.Flags().GetBool("restore"); restore && len(args) == 0 {
			return runSecretRestore(cmd)
		}
		return requireSubcommand(cmd, args)
	},
}

func init() {
	secretCmd.Flags().Bool("restore", false, "Replace every ${secret:name} in the config files with its decrypted value")
	secretCmd.Flags().String("file", "", "With --restore: the one file to rewrite (default: every .yaml / .yml under config/)")

	secretInit := &cobra.Command{
		Use:          "init",
		Short:        "Create .hyp/crypt.yaml if missing and generate the key for its method",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runSecretInit,
	}
	secretInit.Flags().String("method", "", "Encryption method when creating crypt.yaml (aes-256-gcm, chacha20-poly1305, passphrase, age)")

	secretSet := &cobra.Command{
		Use:   "set <name>",
		Short: "Encrypt a value and store it (read from the terminal or stdin, never from arguments)",
		// 值不接受命令列參數：會留在 shell 歷史與行程清單裡
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runSecretSet,
	}
	secretGet := &cobra.Command{
		Use:          "get <name>",
		Short:        "Decrypt a value and print it",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runSecretGet,
	}
	secretList := &cobra.Command{
		Use:          "list",
		Short:        "List stored names (values are not shown; no key needed)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runSecretList,
	}
	secretRm := &cobra.Command{
		Use:          "rm <name>",
		Short:        "Remove a value (no key needed)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         runSecretRm,
	}
	secretKeygen := &cobra.Command{
		Use:          "keygen",
		Short:        "Generate a key file for the method in crypt.yaml (never overwrites)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runSecretKeygen,
	}
	secretKeygen.Flags().StringP("output", "o", "", "Key file path (default: the path configured in crypt.yaml)")

	secretRotate := &cobra.Command{
		Use:   "rotate",
		Short: "Re-encrypt key.secret with the current crypt.yaml settings",
		Long: `Decrypt every value with the current key and re-encrypt it using .hyp/crypt.yaml.
Use it after changing method or age.recipients, or to replace the key:

  hyp secret keygen -o new.key
  hyp secret rotate --new-key-file new.key

With passphrase, the new passphrase comes from $HYPGO_NEW_PASSPHRASE or a prompt.
key.secret is left untouched if any step fails. After rotating, update the key
wherever it is deployed.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runSecretRotate,
	}
	secretRotate.Flags().String("new-key-file", "", "New key file (aes-256-gcm / chacha20-poly1305)")

	secretCmd.AddCommand(secretInit, secretSet, secretGet, secretList, secretRm, secretKeygen, secretRotate)
}

// secretSettings 讀取目前專案的 .hyp/crypt.yaml，並確認金鑰檔沒有被 git 追蹤
func secretSettings(cmd *cobra.Command) (*secret.Settings, error) {
	settings, err := secret.LoadSettings(secret.FindRoot("."))
	if err != nil {
		return nil, err
	}
	return settings, checkKeyFile(cmd, settings)
}

// checkKeyFile 金鑰檔在專案內而且被 git 追蹤時回錯：金鑰與密文一起進版控，加密就沒有作用。
// 仍在使用 v0.9.3 之前的位置（家目錄）時提示搬移
func checkKeyFile(cmd *cobra.Command, settings *secret.Settings) error {
	var path, configured, ext, def string
	var err error
	switch settings.Method {
	case secret.MethodPassphrase:
		return nil
	case secret.MethodAge:
		path, err = settings.IdentityFilePath()
		configured, ext, def = settings.Age.IdentityFile, ".age", secret.DefaultIdentityFile
	default:
		path, err = settings.KeyFilePath()
		configured, ext, def = settings.Key.File, ".key", secret.DefaultKeyFile
	}
	if err != nil {
		return err
	}
	rel, rerr := filepath.Rel(settings.Root(), path)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if configured == "" && path == settings.LegacyKeyPath(ext) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Note: using the key at %s (the location before v0.9.3). Move it to %s to keep it with the project.\n", path, def)
		}
		return nil
	}
	if gitTracks(settings.Root(), rel) {
		rel = filepath.ToSlash(rel)
		return fmt.Errorf("%s is tracked by git: anyone with the repository can decrypt %s.\n"+
			"Run: git rm --cached %s   (and commit), make sure .gitignore has %s,\n"+
			"then replace the key: hyp secret keygen -o new.key && hyp secret rotate --new-key-file new.key",
			rel, settings.SecretFile, rel, secret.KeyIgnorePattern)
	}
	return nil
}

// gitTracks 回報 rel 是否在 git 的索引裡；不是 git repo 或沒有 git 時為 false
func gitTracks(root, rel string) bool {
	return exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel)).Run() == nil
}

// ensureKeyIgnored 把金鑰檔的樣式加進 .gitignore（沒有就建立）；
// 專案有 Dockerfile 或 .dockerignore 時 .dockerignore 也加，金鑰不會被 COPY 進映像
func ensureKeyIgnored(cmd *cobra.Command, root string) error {
	files := []string{".gitignore"}
	if fileExists(filepath.Join(root, ".dockerignore")) || fileExists(filepath.Join(root, "Dockerfile")) {
		files = append(files, ".dockerignore")
	}
	for _, name := range files {
		added, err := secret.EnsureIgnored(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if added {
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %s\n", secret.KeyIgnorePattern, name)
		}
	}
	return nil
}

func openSecretStore(cmd *cobra.Command) (*secret.Store, error) {
	settings, err := secretSettings(cmd)
	if err != nil {
		return nil, err
	}
	return secret.Open(settings, secret.Options{Prompt: secretPrompt(cmd)})
}

// stdinTerminal 回傳 stdin 的終端機檔案；不是終端機時回傳 nil
func stdinTerminal(cmd *cobra.Command) *os.File {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return f
	}
	return nil
}

// readHidden 不回顯地讀一行（提示寫到 stderr，不混進 stdout）
func readHidden(in *os.File, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// secretPrompt 詢問密碼；stdin 不是終端機時回傳 nil（只讀環境變數）
func secretPrompt(cmd *cobra.Command) func(confirm bool) (string, error) {
	in := stdinTerminal(cmd)
	if in == nil {
		return nil
	}
	return func(confirm bool) (string, error) {
		if !confirm {
			return readHidden(in, "Passphrase: ")
		}
		first, err := readHidden(in, "New passphrase: ")
		if err != nil {
			return "", err
		}
		second, err := readHidden(in, "Confirm passphrase: ")
		if err != nil {
			return "", err
		}
		if first != second {
			return "", errors.New("passphrases do not match")
		}
		return first, nil
	}
}

// readSecretValue 從終端機（不回顯）或 stdin 讀取機密值
func readSecretValue(cmd *cobra.Command, name string) (string, error) {
	var value string
	if in := stdinTerminal(cmd); in != nil {
		v, err := readHidden(in, "Value for "+name+": ")
		if err != nil {
			return "", err
		}
		value = v
	} else {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxSecretValue+1))
		if err != nil {
			return "", err
		}
		if len(data) > maxSecretValue {
			return "", fmt.Errorf("value is larger than %d bytes", maxSecretValue)
		}
		// echo 與多數編輯器會在結尾加換行，它不屬於密碼本身
		value = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	}
	if value == "" {
		return "", errors.New("value is empty")
	}
	return value, nil
}

func runSecretInit(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	root := secret.FindRoot(".")
	cryptPath := filepath.Join(root, ".hyp", "crypt.yaml")
	method, _ := cmd.Flags().GetString("method")
	// 寫檔前先驗證：先前打錯的 --method 會先寫進 crypt.yaml 才回錯，之後 init 一直失敗到手動改檔為止
	method = strings.ToLower(strings.TrimSpace(method))
	if method != "" && !secret.ValidMethod(method) {
		return fmt.Errorf("unknown method %q (use %s, %s, %s or %s)", method,
			secret.MethodAES, secret.MethodChaCha, secret.MethodPassphrase, secret.MethodAge)
	}

	content := scaffold.CryptYamlTemplate
	if _, err := os.Stat(cryptPath); errors.Is(err, os.ErrNotExist) {
		if method != "" {
			content = strings.Replace(content, "\nmethod: "+secret.MethodAES+"\n", "\nmethod: "+method+"\n", 1)
		}
		if err := os.MkdirAll(filepath.Dir(cryptPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(cryptPath, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "Created %s\n", cryptPath)
	}

	settings, err := secret.LoadSettings(root)
	if err != nil {
		return err
	}
	if method != "" && settings.Method != method {
		return fmt.Errorf("crypt.yaml already uses %s: change method there, then run hyp secret rotate", settings.Method)
	}

	// 先確保金鑰檔會被忽略，再產生金鑰
	if err := ensureKeyIgnored(cmd, root); err != nil {
		return err
	}
	if err := checkKeyFile(cmd, settings); err != nil {
		return err
	}
	switch settings.Method {
	case secret.MethodPassphrase:
		fmt.Fprintf(out, "Method passphrase: no key file. Set $%s, or enter it when prompted by hyp secret.\n", settings.Passphrase.Env)
	case secret.MethodAge:
		if err := initAge(cmd, settings, cryptPath); err != nil {
			return err
		}
	default:
		if os.Getenv(settings.Key.Env) != "" {
			fmt.Fprintf(out, "Using the key from $%s.\n", settings.Key.Env)
			break
		}
		path, err := settings.KeyFilePath()
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(out, "Key file already exists: %s\n", path)
			break
		}
		if err := writeNewKey(cmd, path); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Next: hyp secret set <name>, then reference it in config.yaml as ${secret:<name>}\n")
	return nil
}

func writeNewKey(cmd *cobra.Command, path string) error {
	key, err := secret.GenerateKey()
	if err != nil {
		return err
	}
	if err := secret.WriteKeyFile(path, key); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Generated key: %s\nNever commit it. Back it up and share it out of band: without it, key.secret cannot be decrypted.\n", path)
	return nil
}

// recipientsLine 比對模板的 recipients: []，寫入公鑰時保留 crypt.yaml 其餘內容與註解
var recipientsLine = regexp.MustCompile(`(?m)^(\s*)recipients:\s*\[\]\s*\r?$`)

// initAge 產生（或沿用）私鑰，並把公鑰寫進 crypt.yaml
func initAge(cmd *cobra.Command, settings *secret.Settings, cryptPath string) error {
	out := cmd.OutOrStdout()
	if len(settings.Age.Recipients) > 0 {
		fmt.Fprintf(out, "age.recipients already set in crypt.yaml.\n")
		return nil
	}
	path, err := settings.IdentityFilePath()
	if err != nil {
		return err
	}
	var recipient string
	if data, err := os.ReadFile(path); err == nil {
		if recipient, err = secret.RecipientFromIdentity(string(data)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Using existing identity: %s\n", path)
	} else {
		var identity string
		if identity, recipient, err = secret.GenerateAgeIdentity(); err != nil {
			return err
		}
		if err := secret.WriteKeyFile(path, identity); err != nil {
			return err
		}
		fmt.Fprintf(out, "Generated identity: %s\nNever commit it. Back it up: without it, key.secret cannot be decrypted.\n", path)
	}

	data, err := os.ReadFile(cryptPath)
	if err != nil {
		return err
	}
	if !recipientsLine.Match(data) {
		fmt.Fprintf(out, "Add this recipient to age.recipients in crypt.yaml: %s\n", recipient)
		return nil
	}
	updated := recipientsLine.ReplaceAll(data, []byte("${1}recipients:\n${1}  - \""+recipient+"\""))
	if err := os.WriteFile(cryptPath, updated, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added recipient to crypt.yaml: %s\n", recipient)
	return nil
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	store, err := openSecretStore(cmd)
	if err != nil {
		return err
	}
	value, err := readSecretValue(cmd, args[0])
	if err != nil {
		return err
	}
	if err := store.Set(args[0], value); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Stored %s (%s). Reference it as ${secret:%s}\n", args[0], store.Method(), args[0])
	return nil
}

func runSecretGet(cmd *cobra.Command, args []string) error {
	store, err := openSecretStore(cmd)
	if err != nil {
		return err
	}
	value, err := store.Get(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), value)
	return nil
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	store, err := openSecretStore(cmd)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "method: %s\n", store.Method())
	for _, name := range store.Names() {
		fmt.Fprintln(&buf, name)
	}
	_, err = cmd.OutOrStdout().Write(buf.Bytes())
	return err
}

func runSecretRm(cmd *cobra.Command, args []string) error {
	store, err := openSecretStore(cmd)
	if err != nil {
		return err
	}
	if err := store.Delete(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", args[0])
	return nil
}

func runSecretKeygen(cmd *cobra.Command, _ []string) error {
	settings, err := secretSettings(cmd)
	if err != nil {
		return err
	}
	path, _ := cmd.Flags().GetString("output")
	if path == "" && settings.Method != secret.MethodPassphrase {
		// 預設位置在專案的 .hyp/ 下：先確保它被忽略
		if err := ensureKeyIgnored(cmd, settings.Root()); err != nil {
			return err
		}
	}
	switch settings.Method {
	case secret.MethodPassphrase:
		return errors.New("method passphrase has no key file")
	case secret.MethodAge:
		if path == "" {
			if path, err = settings.IdentityFilePath(); err != nil {
				return err
			}
		}
		identity, recipient, err := secret.GenerateAgeIdentity()
		if err != nil {
			return err
		}
		if err := secret.WriteKeyFile(path, identity); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Generated identity: %s\nRecipient: %s\n", path, recipient)
	default:
		if path == "" {
			if path, err = settings.KeyFilePath(); err != nil {
				return err
			}
		}
		if err := writeNewKey(cmd, path); err != nil {
			return err
		}
	}
	return nil
}
func runSecretRotate(cmd *cobra.Command, _ []string) error {
	settings, err := secretSettings(cmd)
	if err != nil {
		return err
	}
	var ropts secret.RotateOptions
	var newKey []byte // 新金鑰檔的內容
	newKeyFile, _ := cmd.Flags().GetString("new-key-file")
	if newKeyFile != "" {
		if newKey, err = os.ReadFile(newKeyFile); err != nil {
			return err
		}
		if ropts.NewKey, err = secret.DecodeKey(string(newKey)); err != nil {
			return fmt.Errorf("%s: %w", newKeyFile, err)
		}
	}
	// 舊金鑰來自金鑰檔（不是環境變數）時，rotate 之後要把那個檔案換成新金鑰
	var keyFile string
	if ropts.NewKey != nil && os.Getenv(settings.Key.Env) == "" {
		if p, perr := settings.KeyFilePath(); perr == nil && fileExists(p) {
			keyFile = p
		}
	}
	n, err := secret.Rotate(settings, secret.Options{Prompt: secretPrompt(cmd)}, ropts)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Re-encrypted %d value(s) with %s.\n", n, settings.Method)
	if keyFile != "" {
		// 先前金鑰檔仍是舊金鑰：下一個 hyp secret get 回報 key does not match，要自己把新金鑰搬過去
		old, err := swapKeyFile(keyFile, strings.TrimSpace(string(newKey)))
		if err != nil {
			return fmt.Errorf("%s was re-encrypted but the key file was not replaced: copy %s to %s yourself: %w",
				settings.SecretFile, newKeyFile, keyFile, err)
		}
		fmt.Fprintf(out, "Key file now holds the new key: %s\nThe old key was kept at:       %s (delete it once nothing needs it)\n", keyFile, old)
	}
	fmt.Fprintln(out, "Update the key wherever it is deployed.")
	return nil
}

// swapKeyFile 把目前的金鑰檔改名為 .old（已存在時加上時間，不覆寫任何金鑰），再把新金鑰寫到原位
func swapKeyFile(path, newKey string) (old string, err error) {
	old = path + ".old"
	if _, serr := os.Stat(old); serr == nil {
		old += "." + time.Now().Format("20060102-150405")
	}
	if err := os.Rename(path, old); err != nil {
		return "", err
	}
	if err := secret.WriteKeyFile(path, newKey); err != nil {
		os.Rename(old, path) // 還原，金鑰檔不能消失
		return "", err
	}
	return old, nil
}
