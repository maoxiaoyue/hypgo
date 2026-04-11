package manifest

import (
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// EnrichConfig 控制 Manifest 自動推斷行為
type EnrichConfig struct {
	// InferSummary 從 handler 名、路徑、命令推斷 Summary（預設 true）
	InferSummary bool

	// InferTags 從路徑推斷 Tags（預設 true）
	InferTags bool

	// IncludeFields 在 manifest 中包含 Input/Output 的欄位資訊（預設 false）
	IncludeFields bool

	// LLMEnricher 可選的 LLM 增強器（nil 表示不使用）
	LLMEnricher LLMEnricher
}

// DefaultEnrichConfig 回傳預設配置（純推斷，零成本）
func DefaultEnrichConfig() EnrichConfig {
	return EnrichConfig{
		InferSummary: true,
		InferTags:    true,
	}
}

// LLMEnricher 定義 LLM 增強的介面
// 實作者可以呼叫 OpenAI、Claude、Gemini 等 API
type LLMEnricher interface {
	// EnrichRoute 為指定路由生成更精確的描述
	// 接收現有的 RouteManifest（含推斷結果），回傳增強後的版本
	EnrichRoute(route RouteManifest) RouteManifest
}

// FieldInfo 描述 struct 中的單一欄位（用於 manifest 輸出）
type FieldInfo struct {
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type" yaml:"type"`
	Required bool   `json:"required" yaml:"required"`
}

// enrichRoute 用智慧推斷填補 RouteManifest 的空白欄位
func enrichRoute(rm *RouteManifest, schemaRoute *schema.Route, cfg EnrichConfig) {
	// 推斷 Summary
	if cfg.InferSummary && rm.Summary == "" {
		rm.Summary = inferSummary(rm)
	}

	// 推斷 Tags
	if cfg.InferTags && len(rm.Tags) == 0 {
		rm.Tags = inferTags(rm)
	}

	// 填入欄位資訊
	if cfg.IncludeFields && schemaRoute != nil {
		if schemaRoute.Input != nil {
			rm.InputFields = schema.FieldsOf(schemaRoute.Input)
		}
		if schemaRoute.Output != nil {
			rm.OutputFields = schema.FieldsOf(schemaRoute.Output)
		}
	}

	// LLM 增強（可選）
	if cfg.LLMEnricher != nil {
		*rm = cfg.LLMEnricher.EnrichRoute(*rm)
	}
}

// inferSummary 從 handler 名、路徑、命令推斷 Summary
func inferSummary(rm *RouteManifest) string {
	proto := rm.Protocol
	if proto == "" {
		proto = "rest"
	}

	switch proto {
	case "rest":
		return inferRESTSummary(rm)
	case "grpc":
		return inferGRPCSummary(rm.Command)
	case "bot":
		if rm.Platform != "" {
			return rm.Platform + " command: " + rm.Command
		}
		return "Bot command: " + rm.Command
	case "mcp":
		return "MCP tool: " + humanize(rm.Command)
	case "websocket":
		return "WebSocket: " + humanize(rm.Command)
	case "cli":
		return "CLI command: " + rm.Command
	default:
		return ""
	}
}

// inferRESTSummary 從 handler 名和路徑推斷 REST 路由的 Summary
func inferRESTSummary(rm *RouteManifest) string {
	// 優先從 handler 名推斷
	if len(rm.HandlerNames) > 0 {
		name := rm.HandlerNames[0]
		// "controllers.(*UserController).Create" → "Create User"
		if parts := strings.Split(name, "."); len(parts) >= 2 {
			method := parts[len(parts)-1]
			// 嘗試從 controller 名取得資源名
			for _, p := range parts {
				p = strings.Trim(p, "(*)")
				if strings.HasSuffix(p, "Controller") {
					resource := strings.TrimSuffix(p, "Controller")
					return humanizeMethod(method) + " " + resource
				}
			}
			return humanizeMethod(method)
		}
	}

	// 從路徑推斷
	if rm.Path != "" {
		resource := extractResource(rm.Path)
		if resource != "" {
			switch rm.Method {
			case "GET":
				if strings.Contains(rm.Path, ":") {
					return "Get " + resource
				}
				return "List " + resource
			case "POST":
				return "Create " + resource
			case "PUT", "PATCH":
				return "Update " + resource
			case "DELETE":
				return "Delete " + resource
			}
		}
	}

	return ""
}

// inferGRPCSummary 從 gRPC 命令推斷 Summary
func inferGRPCSummary(command string) string {
	// "UserService/CreateUser" → "Create User (gRPC)"
	parts := strings.Split(command, "/")
	if len(parts) == 2 {
		method := parts[1]
		return humanizeMethod(method) + " (gRPC)"
	}
	return "gRPC: " + command
}

// inferTags 從路徑推斷 Tags
func inferTags(rm *RouteManifest) []string {
	if rm.Path != "" {
		resource := extractResource(rm.Path)
		if resource != "" {
			return []string{strings.ToLower(resource)}
		}
	}

	// 非 REST：從 command 推斷
	if rm.Command != "" {
		parts := strings.Split(rm.Command, "/")
		if len(parts) >= 2 {
			// "UserService/CreateUser" → ["userservice"]
			return []string{strings.ToLower(parts[0])}
		}
	}

	if rm.Platform != "" {
		return []string{rm.Platform}
	}

	return nil
}

// extractResource 從路徑中提取資源名
// "/api/users/:id" → "users"
// "/api/v1/orders" → "orders"
func extractResource(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" || strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "*") {
			continue
		}
		// 跳過 "api", "v1", "v2" 等前綴
		lower := strings.ToLower(seg)
		if lower == "api" || (len(lower) >= 2 && lower[0] == 'v' && lower[1] >= '0' && lower[1] <= '9') {
			continue
		}
		return capitalize(seg)
	}
	return ""
}

// humanize 將 snake_case 或 camelCase 轉為可讀字串
func humanize(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	if len(s) > 0 {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}

// humanizeMethod 將方法名轉為可讀字串
// "CreateUser" → "Create User"
// "GetByID" → "Get By ID"
func humanizeMethod(method string) string {
	var result []rune
	for i, r := range method {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, ' ')
		}
		result = append(result, r)
	}
	return string(result)
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
