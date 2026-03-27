package contract

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// generateTestCase 根據 Route schema 自動生成測試案例
func generateTestCase(route schema.Route) TestCase {
	tc := TestCase{
		Route:        route.Method + " " + resolvePath(route.Path),
		ExpectSchema: route.Output != nil,
	}

	// 生成請求 body
	if route.Input != nil && needsBody(route.Method) {
		tc.Input = generateMinimalJSON(route.Input)
	}

	// 判斷期望狀態碼
	tc.ExpectStatus = guessExpectedStatus(route)

	return tc
}

// generateMinimalJSON 根據 struct 型別生成最小有效 JSON
// 為必填欄位填入合理的預設值
func generateMinimalJSON(typ interface{}) string {
	if typ == nil {
		return "{}"
	}

	t := reflect.TypeOf(typ)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return "{}"
	}

	fields := make(map[string]interface{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonTag := f.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName := f.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
		}

		fields[jsonName] = zeroValueForType(f.Type)
	}

	data, err := json.Marshal(fields)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// resolvePath 將路徑中的參數替換為測試值
// :id → 1, :name → test, *filepath → /test.txt
func resolvePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			name := part[1:]
			parts[i] = guessParamValue(name)
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "test.txt"
		}
	}
	return strings.Join(parts, "/")
}

// guessParamValue 根據參數名猜測合理值
func guessParamValue(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "id") {
		return "1"
	}
	if strings.Contains(lower, "slug") {
		return "test-slug"
	}
	return "test"
}

// guessExpectedStatus 根據路由資訊猜測期望的狀態碼
func guessExpectedStatus(route schema.Route) int {
	// 優先使用明確宣告的 Responses
	if len(route.Responses) > 0 {
		// 取最小的成功狀態碼（2xx）
		for code := 200; code < 300; code++ {
			if _, ok := route.Responses[code]; ok {
				return code
			}
		}
	}

	// 根據 HTTP method 猜測
	switch route.Method {
	case "POST":
		return 201
	case "DELETE":
		return 204
	default:
		return 200
	}
}

// needsBody 判斷 HTTP method 是否需要 body
func needsBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// zeroValueForType 為 Go 型別生成合理的測試值
func zeroValueForType(t reflect.Type) interface{} {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "test"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return 0
	case reflect.Float32, reflect.Float64:
		return 0.0
	case reflect.Bool:
		return false
	case reflect.Slice, reflect.Array:
		return []interface{}{}
	case reflect.Map:
		return map[string]interface{}{}
	default:
		return nil
	}
}
