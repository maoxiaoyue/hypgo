package secret

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
)

var refPattern = regexp.MustCompile(`\$\{secret:([^}]*)\}`)

// ExpandConfig 把設定結構內所有字串裡的 ${secret:name} 換成解密後的值（可嵌在字串中間，
// 例如 "app:${secret:db_pass}@tcp(db:3306)/app"）。專案根目錄由 configFile 往上找。
// 沒有任何引用時完全不碰金鑰與 key.secret。錯誤訊息只含欄位路徑與名稱，不含機密值
func ExpandConfig(cfg interface{}, configFile string) error {
	if os.Getenv(OptionalEnv) != "" {
		return nil
	}
	var store *Store
	return walkStrings(reflect.ValueOf(cfg), "", func(path, value string) (string, error) {
		var firstErr error
		expanded := refPattern.ReplaceAllStringFunc(value, func(ref string) string {
			if firstErr != nil {
				return ref
			}
			if store == nil {
				s, err := openForConfig(configFile)
				if err != nil {
					firstErr = err
					return ref
				}
				store = s
			}
			name := refPattern.FindStringSubmatch(ref)[1]
			plain, err := store.Get(name)
			if err != nil {
				firstErr = err
				return ref
			}
			return plain
		})
		if firstErr != nil {
			return "", fmt.Errorf("config field %s: %w", path, firstErr)
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
