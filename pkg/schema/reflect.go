package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// FieldInfo 描述 struct 中的單一欄位
type FieldInfo struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type" yaml:"type"`
	JSONTag  string `json:"json_tag,omitempty" yaml:"json_tag,omitempty"`
	Required bool   `json:"required" yaml:"required"`
}

// TypeName 返回型別的短名稱
// 例如：TypeName(MyStruct{}) → "MyStruct"
func TypeName(v interface{}) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" {
		return t.String()
	}
	return name
}

// FieldsOf 返回 struct 的所有欄位資訊
// 支援 json tag 解析，自動判斷 required（非 pointer 且無 omitempty）
func FieldsOf(v interface{}) []FieldInfo {
	if v == nil {
		return nil
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	fields := make([]FieldInfo, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		jsonTag := f.Tag.Get("json")
		jsonName := f.Name
		omitempty := false

		if jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
			for _, opt := range parts[1:] {
				if opt == "omitempty" {
					omitempty = true
				}
			}
		} else if jsonTag == "-" {
			continue
		}

		required := !omitempty && f.Type.Kind() != reflect.Ptr

		fields = append(fields, FieldInfo{
			Name:     jsonName,
			Type:     goTypeToSchemaType(f.Type),
			JSONTag:  jsonTag,
			Required: required,
		})
	}
	return fields
}

// ValidateJSON 驗證 JSON 資料是否符合指定的 Go struct 型別
// 檢查 JSON 能否成功反序列化，並驗證必填欄位是否存在
func ValidateJSON(data []byte, typ interface{}) error {
	if typ == nil {
		return nil
	}
	if len(data) == 0 {
		return fmt.Errorf("empty JSON data")
	}

	t := reflect.TypeOf(typ)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 建立新實例並嘗試反序列化
	instance := reflect.New(t).Interface()
	if err := json.Unmarshal(data, instance); err != nil {
		return fmt.Errorf("JSON does not match schema %s: %w", t.Name(), err)
	}

	// 驗證必填欄位
	return validateRequiredFields(data, t)
}

// validateRequiredFields 檢查 JSON 中是否包含所有必填欄位
func validateRequiredFields(data []byte, t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // 已在上層驗證過
	}

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
		omitempty := false
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonName = parts[0]
			}
			for _, opt := range parts[1:] {
				if opt == "omitempty" {
					omitempty = true
				}
			}
		}

		required := !omitempty && f.Type.Kind() != reflect.Ptr
		if required {
			if _, exists := raw[jsonName]; !exists {
				return fmt.Errorf("missing required field: %s", jsonName)
			}
		}
	}
	return nil
}

// GenerateZeroJSON 根據 struct 型別生成零值 JSON
// 用於 Contract Testing 自動生成測試資料
func GenerateZeroJSON(typ interface{}) []byte {
	if typ == nil {
		return []byte("{}")
	}
	t := reflect.TypeOf(typ)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	instance := reflect.New(t).Interface()
	data, err := json.Marshal(instance)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// goTypeToSchemaType 將 Go 型別轉為 schema 型別字串
func goTypeToSchemaType(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return t.String()
	}
}
