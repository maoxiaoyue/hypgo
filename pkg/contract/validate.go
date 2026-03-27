package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// validateResponse 驗證回應 body 是否符合 Output schema
func validateResponse(body []byte, outputType interface{}) error {
	if outputType == nil {
		return nil
	}
	if len(body) == 0 {
		return fmt.Errorf("response body is empty")
	}

	t := reflect.TypeOf(outputType)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 嘗試反序列化
	instance := reflect.New(t).Interface()
	if err := json.Unmarshal(body, instance); err != nil {
		return fmt.Errorf("response does not match Output schema %s: %w", t.Name(), err)
	}

	// 驗證必填欄位
	return validateRequiredFields(body, t)
}

// validateRequest 驗證請求 body 是否符合 Input schema
func validateRequest(body []byte, inputType interface{}) error {
	if inputType == nil {
		return nil
	}
	if len(body) == 0 {
		return fmt.Errorf("request body is empty")
	}

	t := reflect.TypeOf(inputType)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	instance := reflect.New(t).Interface()
	if err := json.Unmarshal(body, instance); err != nil {
		return fmt.Errorf("request does not match Input schema %s: %w", t.Name(), err)
	}

	return validateRequiredFields(body, t)
}

// validateRequiredFields 檢查必填欄位是否存在於 JSON 中
func validateRequiredFields(body []byte, t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}

	var missing []string
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
				missing = append(missing, jsonName)
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}
