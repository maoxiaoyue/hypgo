package migrate

import (
	"reflect"
	"strings"
)

// TableSchema 描述一張資料表的結構
type TableSchema struct {
	Name    string         `json:"name"`
	Columns []ColumnSchema `json:"columns"`
}

// ColumnSchema 描述一個欄位
type ColumnSchema struct {
	Name          string `json:"name"`
	GoType        string `json:"go_type"`
	SQLType       string `json:"sql_type,omitempty"`
	PrimaryKey    bool   `json:"primary_key,omitempty"`
	AutoIncrement bool   `json:"auto_increment,omitempty"`
	NotNull       bool   `json:"not_null,omitempty"`
	Default       string `json:"default,omitempty"`
	Unique        bool   `json:"unique,omitempty"`
}

// ScanModels 掃描所有已註冊的 Model，提取 TableSchema
func ScanModels(registry *ModelRegistry) []TableSchema {
	tables := make([]TableSchema, 0, len(registry.models))
	for _, model := range registry.models {
		if ts := ScanModel(model); ts != nil {
			tables = append(tables, *ts)
		}
	}
	return tables
}

// ScanModel 掃描單一 Model struct，提取 TableSchema
// 解析 bun:"..." tag 來取得表名、欄位名、約束等資訊
func ScanModel(model interface{}) *TableSchema {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	ts := &TableSchema{
		Name:    toSnakeCase(t.Name()),
		Columns: make([]ColumnSchema, 0),
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		// 處理 bun.BaseModel（提取表名）
		bunTag := f.Tag.Get("bun")
		if f.Name == "BaseModel" && bunTag != "" {
			if tableName := extractTableName(bunTag); tableName != "" {
				ts.Name = tableName
			}
			continue
		}

		// 跳過非導出欄位
		if !f.IsExported() {
			continue
		}

		// 跳過 bun:"-"
		if bunTag == "-" {
			continue
		}

		col := parseColumnFromField(f)
		if col != nil {
			ts.Columns = append(ts.Columns, *col)
		}
	}

	return ts
}

// parseColumnFromField 從 struct field 解析 ColumnSchema
func parseColumnFromField(f reflect.StructField) *ColumnSchema {
	bunTag := f.Tag.Get("bun")

	col := &ColumnSchema{
		Name:   toSnakeCase(f.Name),
		GoType: goTypeName(f.Type),
	}

	if bunTag == "" {
		return col
	}

	parts := strings.Split(bunTag, ",")

	// 第一個 part 是欄位名（如果有的話）
	if len(parts) > 0 && parts[0] != "" && !isOption(parts[0]) {
		col.Name = parts[0]
	}

	// 解析選項
	for _, opt := range parts {
		opt = strings.TrimSpace(opt)
		switch {
		case opt == "pk":
			col.PrimaryKey = true
		case opt == "autoincrement":
			col.AutoIncrement = true
		case opt == "notnull":
			col.NotNull = true
		case opt == "unique":
			col.Unique = true
		case strings.HasPrefix(opt, "type:"):
			col.SQLType = strings.TrimPrefix(opt, "type:")
		case strings.HasPrefix(opt, "default:"):
			col.Default = strings.TrimPrefix(opt, "default:")
		}
	}

	return col
}

// extractTableName 從 bun tag 提取表名
// 例如 "table:users,alias:u" → "users"
func extractTableName(tag string) string {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "table:") {
			return strings.TrimPrefix(part, "table:")
		}
	}
	return ""
}

// isOption 判斷是否為 bun tag 選項（非欄位名）
func isOption(s string) bool {
	options := []string{"pk", "autoincrement", "notnull", "unique", "nullzero",
		"scanonly", "extend", "soft_delete"}
	for _, opt := range options {
		if s == opt {
			return true
		}
	}
	return strings.HasPrefix(s, "type:") || strings.HasPrefix(s, "default:") ||
		strings.HasPrefix(s, "table:") || strings.HasPrefix(s, "alias:")
}

// toSnakeCase 將 PascalCase 轉為 snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + ('a' - 'A'))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// goTypeName 取得 Go 型別的簡短名稱
func goTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return t.Kind().String()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return t.Kind().String()
	case reflect.Float32, reflect.Float64:
		return t.Kind().String()
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	default:
		return t.String()
	}
}
