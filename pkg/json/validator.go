package json

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

type Validator struct {
	validator *validator.Validate
}

func NewValidator() *Validator {
	v := validator.New()

	// 註冊自定義標籤
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return &Validator{validator: v}
}

// ValidatedUnmarshal 解析並驗證 JSON
func (v *Validator) ValidatedUnmarshal(data []byte, dest interface{}) error {
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	if err := v.validator.Struct(dest); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	return nil
}

// TypedUnmarshal 帶類型檢查的解析
func TypedUnmarshal(data []byte, dest interface{}) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	destType := reflect.TypeOf(dest).Elem()
	destValue := reflect.ValueOf(dest).Elem()

	return checkAndSetFields(raw, destType, destValue)
}

func checkAndSetFields(data map[string]interface{}, destType reflect.Type, destValue reflect.Value) error {
	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		fieldValue := destValue.Field(i)

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		fieldName := strings.Split(jsonTag, ",")[0]
		value, ok := data[fieldName]
		if !ok && strings.Contains(jsonTag, "omitempty") {
			continue
		}

		if err := setFieldValue(fieldValue, value, field); err != nil {
			return fmt.Errorf("field %s: %w", fieldName, err)
		}
	}

	return nil
}

func setFieldValue(fieldValue reflect.Value, value interface{}, field reflect.StructField) error {
	if value == nil {
		return nil
	}

	valueType := reflect.TypeOf(value)
	fieldType := field.Type

	// 檢查類型匹配
	if !valueType.ConvertibleTo(fieldType) {
		return fmt.Errorf("cannot convert %v to %v", valueType, fieldType)
	}

	fieldValue.Set(reflect.ValueOf(value).Convert(fieldType))
	return nil
}

// SchemaValidation 基於 schema 的驗證
type Schema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

type Property struct {
	Type      string   `json:"type"`
	Format    string   `json:"format,omitempty"`
	MinLength *int     `json:"minLength,omitempty"`
	MaxLength *int     `json:"maxLength,omitempty"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
	Enum      []string `json:"enum,omitempty"`
}

func ValidateWithSchema(data []byte, schema Schema) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// 檢查必填欄位
	for _, required := range schema.Required {
		if _, ok := raw[required]; !ok {
			return fmt.Errorf("missing required field: %s", required)
		}
	}

	// 驗證每個欄位
	for name, prop := range schema.Properties {
		value, ok := raw[name]
		if !ok {
			continue
		}

		if err := validateProperty(name, value, prop); err != nil {
			return err
		}
	}

	return nil
}

func validateProperty(name string, value interface{}, prop Property) error {
	switch prop.Type {
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("field %s must be string", name)
		}

		if prop.MinLength != nil && len(str) < *prop.MinLength {
			return fmt.Errorf("field %s length must be at least %d", name, *prop.MinLength)
		}

		if prop.MaxLength != nil && len(str) > *prop.MaxLength {
			return fmt.Errorf("field %s length must not exceed %d", name, *prop.MaxLength)
		}

	case "number":
		num, ok := value.(float64)
		if !ok {
			return fmt.Errorf("field %s must be number", name)
		}

		if prop.Minimum != nil && num < *prop.Minimum {
			return fmt.Errorf("field %s must be at least %f", name, *prop.Minimum)
		}

		if prop.Maximum != nil && num > *prop.Maximum {
			return fmt.Errorf("field %s must not exceed %f", name, *prop.Maximum)
		}

	case "integer":
		_, ok := value.(float64)
		if !ok {
			return fmt.Errorf("field %s must be integer", name)
		}

	case "boolean":
		_, ok := value.(bool)
		if !ok {
			return fmt.Errorf("field %s must be boolean", name)
		}

	case "array":
		_, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("field %s must be array", name)
		}

	case "object":
		_, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("field %s must be object", name)
		}
	}

	return nil
}

// 便利函數
func Marshal(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func MarshalCompact(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
