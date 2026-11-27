package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/secret"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var cryptCmd = &cobra.Command{
	Use:   "crypt --key <path> [--file config/config.yaml]",
	Short: "Encrypt the user/password under a config.yaml key into key.secret",
	Long: `Encrypt the user, username, password and passwd values under a config.yaml key
into key.secret, and replace them in the file with ${secret:name} references.
Comments and formatting in config.yaml are kept.

  database:
    postgresql:
      user: "${secret:database.postgresql.user}"
      password: "${secret:database.postgresql.password}"

Values are decrypted when config.ConfigLoader.Load reads the file into a struct that
has those fields; config.LoadConfig only has the framework's own fields (such as
database.redis). The encryption method comes from .hyp/crypt.yaml (hyp secret init).

Example:
  hyp crypt --key database.postgresql`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runCryptCmd,
}

func init() {
	cryptCmd.Flags().String("file", "config/config.yaml", "YAML file to read and update")
	cryptCmd.Flags().String("key", "", "Dotted path to the section holding the credentials (required)")
}

// credential 為一個待加密的值
type credential struct {
	name  string // key.secret 的名稱，例如 database.postgresql.password
	node  *yaml.Node
	value string // 要寫進檔案的新值
}

// isCredentialField 判斷欄位是否為帳號或密碼
func isCredentialField(field string) bool {
	switch strings.ToLower(field) {
	case "user", "username", "password", "passwd":
		return true
	}
	return false
}

// findCredentials 找出 key 底下的帳號密碼，分成要加密的與略過的（已是引用或空值）。
// 回傳的 node 屬於 doc，之後改動它們即可得到預期的新內容
func findCredentials(doc *yaml.Node, key string) (todo []credential, skipped []string, err error) {
	if doc.Kind != yaml.DocumentNode {
		return nil, nil, fmt.Errorf("key %q not found: file is empty", key)
	}
	section := doc.Content[0]
	for _, part := range strings.Split(key, ".") {
		if section.Kind != yaml.MappingNode {
			return nil, nil, fmt.Errorf("key %q not found", key)
		}
		var next *yaml.Node
		for i := 0; i+1 < len(section.Content); i += 2 {
			if section.Content[i].Value == part {
				next = section.Content[i+1]
				break
			}
		}
		if next == nil {
			return nil, nil, fmt.Errorf("key %q not found", key)
		}
		section = next
	}
	if section.Kind == yaml.AliasNode {
		return nil, nil, fmt.Errorf("key %q is a YAML alias: use the section where it is defined", key)
	}
	if section.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("key %q is not a section with fields", key)
	}

	found := false
	for i := 0; i+1 < len(section.Content); i += 2 {
		field, value := section.Content[i].Value, section.Content[i+1]
		if !isCredentialField(field) {
			continue
		}
		found = true
		name := key + "." + field
		switch {
		case value.Kind != yaml.ScalarNode:
			return nil, nil, fmt.Errorf("%s must be a single value (YAML aliases and nested values are not supported)", name)
		case value.Tag == "!!null" || value.Value == "":
			skipped = append(skipped, name+" (empty)")
		case strings.Contains(value.Value, "${"):
			// 已是 ${secret:name}，或是其他引用：把引用本身加密就壞了
			skipped = append(skipped, name+" (already a reference)")
		default:
			todo = append(todo, credential{name: name, node: value, value: "${secret:" + name + "}"})
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("no user, username, password or passwd under %q (for values such as dsn, use hyp secret set)", key)
	}
	return todo, skipped, nil
}

// scalarEnd 回傳單行純量的結尾位置；值不在這一行結束或位置對不上時回傳 false
func scalarEnd(line []rune, start int, node *yaml.Node) (int, bool) {
	if start < 0 || start >= len(line) {
		return 0, false
	}
	switch line[start] {
	case '"':
		for i := start + 1; i < len(line); i++ {
			switch line[i] {
			case '\\':
				i++
			case '"':
				return i + 1, true
			}
		}
	case '\'':
		for i := start + 1; i < len(line); i++ {
			if line[i] != '\'' {
				continue
			}
			if i+1 < len(line) && line[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, true
		}
	default:
		value := []rune(node.Value)
		end := start + len(value)
		if end <= len(line) && string(line[start:end]) == node.Value {
			return end, true
		}
	}
	return 0, false
}

// replaceCredentials 只替換目標值的文字：以 yaml.Node 整份重新輸出會弄丟空行與註解對齊，
// 手寫的設定檔每次都出現大片 diff。回傳的內容必須與 doc 改動後語意相同，否則視為失敗，
// 設定檔不會被寫壞
func replaceCredentials(data []byte, doc *yaml.Node, creds []credential) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	// 由後往前：同一行有多個值時（{user: a, password: b}），前面的欄位位置才不會變動
	for i := len(creds) - 1; i >= 0; i-- {
		c := creds[i]
		idx := c.node.Line - 1
		if idx < 0 || idx >= len(lines) {
			return nil, unsupportedValue(c)
		}
		line := []rune(lines[idx])
		start := c.node.Column - 1
		end, ok := scalarEnd(line, start, c.node)
		if !ok {
			return nil, unsupportedValue(c)
		}
		// strconv.Quote 的跳脫都是合法的 YAML 雙引號跳脫；下面再以解析結果比對一次
		lines[idx] = string(line[:start]) + strconv.Quote(c.value) + string(line[end:])
		c.node.Value, c.node.Tag, c.node.Style = c.value, "!!str", yaml.DoubleQuotedStyle
	}
	out := []byte(strings.Join(lines, "\n"))

	expected, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var want, got interface{}
	if err := yaml.Unmarshal(expected, &want); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(want, got) {
		return nil, errors.New("could not update the file safely, nothing changed: move the values to plain one-line form and retry")
	}
	return out, nil
}

func unsupportedValue(c credential) error {
	return fmt.Errorf("line %d: %s is not a plain one-line value, nothing changed: rewrite it on one line, or use hyp secret set", c.node.Line, c.name)
}

// findReferences 找出 doc 裡所有含 ${secret:name} 的純量（依文件順序），value 為展開後的明文
func findReferences(doc *yaml.Node, store *secret.Store) ([]credential, error) {
	var refs []credential
	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.ScalarNode && secret.HasReference(n.Value) {
			plain, err := store.Expand(n.Value)
			if err != nil {
				return fmt.Errorf("line %d: %w", n.Line, err)
			}
			refs = append(refs, credential{name: n.Value, node: n, value: plain})
		}
		for _, c := range n.Content { // alias 不往下走：值在定義它的地方
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return refs, walk(doc)
}

// restoreTargets 回傳 --restore 要處理的檔案：有 --file 時照給的，否則是專案 config/ 下所有 YAML
func restoreTargets(root, file string) ([]string, error) {
	if file != "" {
		return []string{file}, nil
	}
	var out []string
	err := filepath.WalkDir(filepath.Join(root, "config"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ext := strings.ToLower(filepath.Ext(p)); !d.IsDir() && (ext == ".yaml" || ext == ".yml") {
			out = append(out, p)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("no config/ directory in this project: name the file with --file")
	}
	return out, err
}

// runSecretRestore 把設定檔裡的 ${secret:name} 換回明文（hyp crypt 的反向）。key.secret 不變。
// 全部檔案都解密、改寫成功後才開始寫入
func runSecretRestore(cmd *cobra.Command) error {
	settings, err := secretSettings(cmd)
	if err != nil {
		return err
	}
	store, err := secret.Open(settings, secret.Options{Prompt: secretPrompt(cmd)})
	if err != nil {
		return err
	}
	file, _ := cmd.Flags().GetString("file")
	targets, err := restoreTargets(settings.Root(), file)
	if err != nil {
		return err
	}

	type change struct {
		path string
		data []byte
		n    int
	}
	var changes []change
	for _, path := range targets {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		refs, err := findReferences(&doc, store)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(refs) == 0 {
			continue
		}
		updated, err := replaceCredentials(data, &doc, refs)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		changes = append(changes, change{path, updated, len(refs)})
	}

	out := cmd.OutOrStdout()
	if len(changes) == 0 {
		fmt.Fprintln(out, "No ${secret:name} references found, nothing changed.")
		return nil
	}
	for _, c := range changes {
		if err := writeFileAtomic(c.path, c.data); err != nil {
			return err
		}
		fmt.Fprintf(out, "Restored %d value(s) in %s\n", c.n, c.path)
		// 設定檔通常在版控裡：指名哪些檔案現在帶著明文。先前只有一句籠統的「不要提交」
		if rel, err := filepath.Rel(settings.Root(), c.path); err == nil && gitTracks(settings.Root(), rel) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s is tracked by git and now holds plaintext secrets; `git diff` shows them and a commit publishes them.\n", filepath.ToSlash(rel))
		}
	}
	fmt.Fprintf(out, "These files now hold the plaintext: do not commit them. %s is unchanged;\n"+
		"put the references back by hand, or with hyp crypt --key <section> for user / password fields.\n", settings.SecretFile)
	return nil
}

// writeFileAtomic 寫入暫存檔再 rename，中途失敗不會留下寫到一半的設定檔
func writeFileAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hyp-crypt-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	return werr
}

func runCryptCmd(cmd *cobra.Command, _ []string) error {
	file, _ := cmd.Flags().GetString("file")
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return errors.New("--key is required, e.g. hyp crypt --key database.postgresql")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	creds, skipped, err := findCredentials(&doc, key)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	out := cmd.OutOrStdout()
	for _, s := range skipped {
		fmt.Fprintf(out, "Skipped %s\n", s)
	}
	if len(creds) == 0 {
		fmt.Fprintln(out, "Nothing to encrypt.")
		return nil
	}

	// 專案根目錄的找法與載入設定時（secret.ExpandConfig）相同，否則執行期會找不到 key.secret
	settings, err := secret.LoadSettings(secret.FindRoot(filepath.Dir(file)))
	if err != nil {
		return err
	}
	if err := checkKeyFile(cmd, settings); err != nil {
		return err
	}
	store, err := secret.Open(settings, secret.Options{Prompt: secretPrompt(cmd)})
	if err != nil {
		return err
	}

	values := make([]string, len(creds))
	for i, c := range creds {
		values[i] = c.node.Value
	}
	updated, err := replaceCredentials(data, &doc, creds)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	// 先存密文再改設定檔：中途失敗時明文還在，不會出現引用了卻沒有值的狀態
	for i, c := range creds {
		verb := "Encrypted"
		if store.Has(c.name) {
			verb = "Replaced"
		}
		if err := store.Set(c.name, values[i]); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s\n", verb, c.name)
	}
	if err := writeFileAtomic(file, updated); err != nil {
		return err
	}

	fmt.Fprintf(out, "Wrote %s (%s) and updated %s\n", settings.Path(), store.Method(), file)
	fmt.Fprintln(out, "If the file was committed before, the old values are still in git history: change those credentials.")
	return nil
}
