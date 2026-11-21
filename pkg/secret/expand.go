package secret

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`\$\{secret:([^}]*)\}`)

// refLookalike 比對寫錯的引用：少了大括號、大小寫不同、secrets、冒號前後有空白
var refLookalike = regexp.MustCompile(`(?i)\$\{?\s*secrets?\s*:`)

// HasReference 回報字串是否含 ${secret:name}
func HasReference(value string) bool { return refPattern.MatchString(value) }

// Expand 把字串裡的 ${secret:name} 換成解密後的值
func (st *Store) Expand(value string) (string, error) {
	return expandString(value, func() (*Store, error) { return st, nil })
}

// expandString 展開一個字串；open 只在真的有引用時才被呼叫。
// 寫錯的引用回錯：先前 ${secret:db_pass（少了 }）、${SECRET:x}、$secret:x、${secrets:x}
// 都載入成功，字面內容直接當成密碼。只檢查設定檔的原文，不檢查解密出來的值
func expandString(value string, open func() (*Store, error)) (string, error) {
	if m := refLookalike.FindString(refPattern.ReplaceAllString(value, "")); m != "" {
		return "", fmt.Errorf("%q looks like a secret reference but is not one: write ${secret:name} (lower case, both braces)", m)
	}
	var firstErr error
	expanded := refPattern.ReplaceAllStringFunc(value, func(ref string) string {
		if firstErr != nil {
			return ref
		}
		name := refPattern.FindStringSubmatch(ref)[1]
		if name != strings.TrimSpace(name) {
			firstErr = fmt.Errorf("secret: reference %q has spaces around the name", ref)
			return ref
		}
		store, err := open()
		if err != nil {
			firstErr = err
			return ref
		}
		plain, err := store.Get(name)
		if err != nil {
			firstErr = err
			return ref
		}
		return plain
	})
	if firstErr != nil {
		return "", firstErr
	}
	return expanded, nil
}

// ExpandConfig 把設定結構內所有字串裡的 ${secret:name} 換成解密後的值（可嵌在字串中間，
// 例如 "app:${secret:db_pass}@tcp(db:3306)/app"）。專案根目錄由 configFile 往上找。
// 沒有任何引用時完全不碰金鑰與 key.secret。錯誤訊息只含欄位路徑與名稱，不含機密值
func ExpandConfig(cfg interface{}, configFile string) error {
	if os.Getenv(OptionalEnv) != "" {
		return nil
	}
	var store *Store
	open := func() (*Store, error) {
		if store == nil {
			s, err := openForConfig(configFile)
			if err != nil {
				return nil, err
			}
			store = s
		}
		return store, nil
	}
	return walkStrings(reflect.ValueOf(cfg), "", func(path, value string) (string, error) {
		expanded, err := expandString(value, open)
		if err != nil {
			return "", fmt.Errorf("config field %s: %w", path, err)
		}
		return expanded, nil
	})
}

func openForConfig(configFile string) (*Store, error) {
	settings, err := LoadSettings(FindRoot(filepath.Dir(configFile)))
	if err != nil {
		return nil, err
	}
	return Open(settings, Options{})
}

// walkStrings 走訪所有可設定的字串（struct 欄位、slice、map 值、interface 內的值）
func walkStrings(v reflect.Value, path string, fn func(path, value string) (string, error)) error {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return walkStrings(v.Elem(), path, fn)
	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		cp := reflect.New(v.Elem().Type()).Elem()
		cp.Set(v.Elem())
		if err := walkStrings(cp, path, fn); err != nil {
			return err
		}
		v.Set(cp)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := walkStrings(v.Field(i), joinPath(path, t.Field(i).Name), fn); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walkStrings(v.Index(i), fmt.Sprintf("%s[%d]", path, i), fn); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			cp := reflect.New(v.Type().Elem()).Elem()
			cp.Set(v.MapIndex(k))
			if err := walkStrings(cp, fmt.Sprintf("%s[%v]", path, k), fn); err != nil {
				return err
			}
			v.SetMapIndex(k, cp)
		}
	case reflect.String:
		if !v.CanSet() {
			return nil
		}
		out, err := fn(path, v.String())
		if err != nil {
			return err
		}
		v.SetString(out)
	}
	return nil
}

func joinPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}
