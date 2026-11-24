package config

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// StrictEnv 設為 1 / true 時，設定檔的任何警告（未知的鍵、空檔案）都讓載入失敗。給 CI 與正式環境的啟動檢查用
const StrictEnv = "HYPGO_CONFIG_STRICT"

// EnvPrefix 是環境變數覆寫的前綴：HYPGO_SERVER_ADDR 覆寫 server.addr
const EnvPrefix = "HYPGO_"

// WarningOutput 是載入設定檔時警告的輸出位置；設為 io.Discard 可關閉
var WarningOutput io.Writer = os.Stderr

// Warning 是設定檔裡不會讓載入失敗、但多半是寫錯的地方
type Warning struct {
	File    string // 設定檔路徑
	Key     string // "server.tls.enable"；檔案層級的警告為空
	Line    int    // 1-based；0 表示不適用
	Message string
}

// String 回傳 "config/config.yaml:12: server.tls.enable: unknown key (did you mean \"enabled\"?)"
func (w Warning) String() string {
	loc := w.File
	if w.Line > 0 {
		loc = fmt.Sprintf("%s:%d", w.File, w.Line)
	}
	if w.Key == "" {
		return fmt.Sprintf("%s: %s", loc, w.Message)
	}
	return fmt.Sprintf("%s: %s: %s", loc, w.Key, w.Message)
}

// strictMode 回報是否啟用嚴格模式
func strictMode() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(StrictEnv)))
	return v != "" && v != "0" && v != "false" && v != "no" && v != "off"
}

// inspectYAML 比對設定檔內容與目標結構，回傳警告與「檔案裡實際寫了哪些鍵」。
//
// 先前打錯的鍵完全沒有反應：tls 底下寫 enable（少一個 d）載入成功而 TLS 關閉，
// sever / adress、少縮排、整個檔案是空的都靜默退回預設值。
//
// allowExtraTop 為 true 時（config.yaml），頂層沒見過的鍵只有在像是打錯（與已知的鍵相近）時才警告：
// 使用者可以在同一個檔案放自己的區段。巢狀的未知鍵一律警告
func inspectYAML(file string, data []byte, out any, allowExtraTop bool) (warnings []Warning, explicit map[string]bool) {
	explicit = map[string]bool{}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, explicit // 語法錯誤由正式的 Unmarshal 回報
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode || len(root.Content[0].Content) == 0 {
		return []Warning{{File: file, Message: "the file has no settings; built-in defaults are used for everything"}}, explicit
	}
	t := reflect.TypeOf(out)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil, explicit
	}
	nested := map[string]string{}
	nestedKeys(t, "", nested)
	walkYAML(file, root.Content[0], t, "", allowExtraTop, nested, &warnings, explicit)
	return warnings, explicit
}

// nestedKeys 收集巢狀鍵的「鍵名 → 完整路徑」（server.addr 的 addr）；同名出現在多處時記成空字串
func nestedKeys(t reflect.Type, path string, out map[string]string) {
	for name, ft := range yamlFields(t) {
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if path != "" {
			if _, dup := out[name]; dup {
				out[name] = ""
			} else {
				out[name] = path + name
			}
		}
		if ft.Kind() == reflect.Struct {
			nestedKeys(ft, path+name+".", out)
		}
	}
}

func walkYAML(file string, node *yaml.Node, t reflect.Type, path string, allowExtra bool, nested map[string]string, warnings *[]Warning, explicit map[string]bool) {
	fields := yamlFields(t)
	for i := 0; i+1 < len(node.Content); i += 2 {
		k, v := node.Content[i], node.Content[i+1]
		// alias（server: *base）指向的內容照常檢查。先前 alias 節點既不是 mapping 也不是 sequence，整段略過
		if v.Kind == yaml.AliasNode && v.Alias != nil {
			v = v.Alias
		}
		// 合併鍵（<<: *defaults）：帶進來的鍵與直接寫的一樣檢查、一樣算「有寫」。
		// 先前被報成未知的鍵 "<<"，而帶進來的 read_timeout: 0 不算明確設定、退回預設值
		if k.ShortTag() == "!!merge" {
			for _, m := range mergeSources(v) {
				walkYAML(file, m, t, path, allowExtra, nested, warnings, explicit)
			}
			continue
		}
		key := path + k.Value
		ft, ok := fields[k.Value]
		if !ok {
			// 頂層的 mapping 是使用者自己的區段（redis:、db:、tls:），只有像錯字時才提示。
			// 先前只要與任何巢狀鍵同名就報「少了縮排」，連帶擋下 hyp lint
			ownSection := allowExtra && v.Kind == yaml.MappingNode
			// 頂層只在很像打錯時才提示（差一個字），避免把使用者自己的區段（service、payment…）當成錯字
			maxDist := min(3, len(k.Value)/3+1)
			if allowExtra {
				maxDist = 1
			}
			suggestion := closestKey(k.Value, fields, maxDist)
			msg := "unknown key, it has no effect"
			switch full, isNested := nested[k.Value]; {
			case suggestion != "":
				msg += fmt.Sprintf(" (did you mean %q?)", suggestion)
			case ownSection:
				continue
			case allowExtra && isNested && full != "":
				// 少了縮排：server 底下的 addr 寫到了最外層
				msg += fmt.Sprintf(" at the top level (did you mean %s? check the indentation)", full)
			case allowExtra && isNested:
				msg += " at the top level (it belongs inside a section; check the indentation)"
			case allowExtra:
				continue // 使用者自己的區段
			}
			*warnings = append(*warnings, Warning{File: file, Key: key, Line: k.Line, Message: msg})
			continue
		}
		// 空值（read_timeout: 或 read_timeout: ~）等於沒寫。先前鍵存在就算明確設定，
		// 值是 0，於是逾時與 max_handlers 被關掉
		if v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null" {
			continue
		}
		explicit[key] = true
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case ft.Kind() == reflect.Struct && v.Kind == yaml.MappingNode:
			walkYAML(file, v, ft, key+".", false, nested, warnings, explicit)
		case ft.Kind() == reflect.Slice && v.Kind == yaml.SequenceNode:
			et := ft.Elem()
			for et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() != reflect.Struct {
				continue
			}
			for j, item := range v.Content {
				if item.Kind == yaml.MappingNode {
					walkYAML(file, item, et, fmt.Sprintf("%s[%d].", key, j), false, nested, warnings, explicit)
				}
			}
		}
	}
}

// mergeSources 回傳合併鍵的來源 mapping：<<: *a 或 <<: [*a, *b]
func mergeSources(v *yaml.Node) []*yaml.Node {
	if v.Kind == yaml.AliasNode && v.Alias != nil {
		v = v.Alias
	}
	switch v.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{v}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range v.Content {
			out = append(out, mergeSources(item)...)
		}
		return out
	}
	return nil
}

// yamlFields 回傳 struct 的「yaml 鍵名 → 欄位型別」。`yaml:",inline"` 的 struct 欄位攤平到同一層：
// 先前把 config.Config 以 inline 嵌進自己的 struct 時，server / logger 全被報成未知的鍵
func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if strings.Contains(","+opts+",", ",inline,") {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for n, nt := range yamlFields(ft) {
					if _, dup := out[n]; !dup {
						out[n] = nt
					}
				}
			}
			continue
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}

// closestKey 回傳與 key 最相近、編輯距離不超過 maxDist 的已知鍵；沒有就回空字串
func closestKey(key string, fields map[string]reflect.Type, maxDist int) string {
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	best, bestDist := "", maxDist+1
	for _, n := range names {
		if d := editDistance(strings.ToLower(key), n); d < bestDist {
			best, bestDist = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// reportWarnings 處理警告：嚴格模式回錯，否則印到 WarningOutput
func reportWarnings(warnings []Warning) error {
	if len(warnings) == 0 {
		return nil
	}
	if strictMode() {
		lines := make([]string, len(warnings))
		for i, w := range warnings {
			lines[i] = w.String()
		}
		return fmt.Errorf("%s is set and the config has %d warning(s):\n  %s", StrictEnv, len(warnings), strings.Join(lines, "\n  "))
	}
	if WarningOutput != nil {
		for _, w := range warnings {
			fmt.Fprintf(WarningOutput, "config warning: %s\n", w)
		}
	}
	return nil
}

// checkDesignConfig 檢查 .hyp/ 下的設計時設定檔（comment.yaml、llm.yaml、config.yaml）：
// 這些檔案沒有使用者自訂的區段，任何未知的鍵都警告
func checkDesignConfig(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // 讀檔錯誤由呼叫端的正式載入回報
	}
	warnings, _ := inspectYAML(path, data, out, false)
	return reportWarnings(warnings)
}

// ApplyEnv 以環境變數覆寫設定：HYPGO_ 加上大寫的鍵路徑，`.` 換成 `_`
// （HYPGO_SERVER_ADDR、HYPGO_SERVER_TLS_ENABLED、HYPGO_DATABASE_DSN、HYPGO_LOGGER_LEVEL…）。
// 支援字串、整數、布林與字串清單（逗號分隔）；值的格式不對時回錯。
// 容器部署時不必為了一個位址或 DSN 改檔或另外掛載設定檔
func (c *Config) ApplyEnv() error {
	return applyEnv(reflect.ValueOf(c).Elem(), "", c)
}

func applyEnv(v reflect.Value, path string, c *Config) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := path + name
		fv := v.Field(i)
		if fv.Kind() == reflect.Struct {
			if err := applyEnv(fv, key+".", c); err != nil {
				return err
			}
			continue
		}
		env := EnvPrefix + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		// 空字串視同沒設定：docker-compose 的 `VAR: ${X}` 在 X 沒設定時正好產生空值。
		// 先前它把 allow_origins: [] 換成 nil（掛回預設的 *）、清掉設定檔的 DSN、讓整數欄位載入失敗
		raw := os.Getenv(env)
		if raw == "" {
			continue
		}
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(raw)
		case reflect.Int, reflect.Int64:
			n, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s=%q: %s must be an integer", env, raw, key)
			}
			fv.SetInt(int64(n))
		case reflect.Bool:
			b, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s=%q: %s must be true or false", env, raw, key)
			}
			fv.SetBool(b)
		case reflect.Slice:
			if fv.Type().Elem().Kind() != reflect.String {
				continue
			}
			var items []string
			for _, s := range strings.Split(raw, ",") {
				if s = strings.TrimSpace(s); s != "" {
					items = append(items, s)
				}
			}
			fv.Set(reflect.ValueOf(items))
		default:
			continue
		}
		c.markExplicit(key)
	}
	return nil
}
