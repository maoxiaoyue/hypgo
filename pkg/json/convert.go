package json

import (
	"encoding/json"
	"fmt"
)

// Map2JSON 將 map 轉換為 JSON 字串。
func Map2JSON(m map[string]interface{}) (string, error) {
	if m == nil {
		return "null", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("map2json: %w", err)
	}
	return string(b), nil
}

// Map2JSONIndent 將 map 轉換為格式化的 JSON 字串。
func Map2JSONIndent(m map[string]interface{}, prefix, indent string) (string, error) {
	if m == nil {
		return "null", nil
	}
	b, err := json.MarshalIndent(m, prefix, indent)
	if err != nil {
		return "", fmt.Errorf("map2json: %w", err)
	}
	return string(b), nil
}

// Map2JSONBytes 將 map 轉換為 JSON bytes。
func Map2JSONBytes(m map[string]interface{}) ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("map2json: %w", err)
	}
	return b, nil
}

// JSON2Map 將 JSON 字串解析為 map。
func JSON2Map(s string) (map[string]interface{}, error) {
	if s == "" {
		return nil, fmt.Errorf("json2map: empty input")
	}
	m := make(map[string]interface{})
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("json2map: %w", err)
	}
	return m, nil
}

// JSON2MapBytes 將 JSON bytes 解析為 map。
func JSON2MapBytes(b []byte) (map[string]interface{}, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("json2map: empty input")
	}
	m := make(map[string]interface{})
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("json2map: %w", err)
	}
	return m, nil
}
