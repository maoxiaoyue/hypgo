// Package annotation 提供 HypGo 標準化 Annotation Protocol
// 定義 // @ai: 前綴的結構化標註格式，讓 AI 從註解理解業務約束、安全需求與依賴關係
package annotation

import (
	"strings"
)

// AnnotationType 標註類型
type AnnotationType string

const (
	// Constraint 業務約束（如最大值、格式限制）
	Constraint AnnotationType = "constraint"
	// Deprecated 標記已棄用，說明替代方案
	Deprecated AnnotationType = "deprecated"
	// Security 安全相關標註（如需要認證、權限）
	Security AnnotationType = "security"
	// Impact 影響範圍標註（關聯的路由、模型）
	Impact AnnotationType = "impact"
	// Owner 負責人/團隊標註
	Owner AnnotationType = "owner"
)

// Annotation 結構化標註
type Annotation struct {
	Type  AnnotationType // 標註類型
	Value string         // 標註值（= 之後的部分）
	Line  int            // 所在行號
}

// prefix 是所有 AI 標註的前綴
const prefix = "@ai:"

// ParseAnnotations 從多行註解文字中提取所有 @ai: 標註
//
// 輸入範例：
//
//	// Create 建立使用者
//	// @ai:constraint max_items=100
//	// @ai:security requires_auth
//
// 回傳：
//
//	[]Annotation{
//	    {Type: Constraint, Value: "max_items=100"},
//	    {Type: Security, Value: "requires_auth"},
//	}
func ParseAnnotations(text string) []Annotation {
	var result []Annotation
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		// 移除 Go 註解前綴
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimSpace(line)

		if !strings.HasPrefix(line, prefix) {
			continue
		}

		rest := line[len(prefix):]
		annType, value := parseAnnotationParts(rest)
		if annType != "" {
			result = append(result, Annotation{
				Type:  annType,
				Value: value,
				Line:  i + 1,
			})
		}
	}

	return result
}

// parseAnnotationParts 解析 @ai: 之後的型別和值
// 例如 "constraint max_items=100" → (Constraint, "max_items=100")
func parseAnnotationParts(s string) (AnnotationType, string) {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, " ", 2)
	if len(parts) == 0 {
		return "", ""
	}

	typStr := strings.ToLower(parts[0])
	value := ""
	if len(parts) > 1 {
		value = strings.TrimSpace(parts[1])
	}

	switch AnnotationType(typStr) {
	case Constraint, Deprecated, Security, Impact, Owner:
		return AnnotationType(typStr), value
	default:
		return "", ""
	}
}

// FormatAnnotation 將 Annotation 格式化為 Go 註解字串
func FormatAnnotation(a Annotation) string {
	if a.Value == "" {
		return "// " + prefix + string(a.Type)
	}
	return "// " + prefix + string(a.Type) + " " + a.Value
}

// ValidAnnotationTypes 回傳所有合法的標註類型
func ValidAnnotationTypes() []AnnotationType {
	return []AnnotationType{Constraint, Deprecated, Security, Impact, Owner}
}
