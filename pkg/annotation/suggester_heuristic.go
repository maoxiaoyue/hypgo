package annotation

import (
	"context"
	"fmt"
	"strings"
)

// HeuristicSuggester 用靜態規則產生 doc 與 @ai 標註，無需 LLM
type HeuristicSuggester struct{}

// NewHeuristicSuggester 建立預設啟發式建議器
func NewHeuristicSuggester() Suggester {
	return &HeuristicSuggester{}
}

func (h *HeuristicSuggester) Suggest(_ context.Context, req SuggestRequest) (Suggestion, error) {
	return Suggestion{
		Doc:         heuristicDoc(req),
		Annotations: heuristicAnnotations(req),
	}, nil
}

// ProviderName 回傳 "heuristic"，標示本地規則而非 LLM
func (h *HeuristicSuggester) ProviderName() string {
	return "heuristic"
}

func heuristicDoc(req SuggestRequest) string {
	switch req.Kind {
	case "package":
		return fmt.Sprintf("Package %s ...", req.PkgName)
	case "type":
		return fmt.Sprintf("%s ...", baseName(req.Name))
	default:
		return fmt.Sprintf("%s ...", baseName(req.Name))
	}
}

func heuristicAnnotations(req SuggestRequest) []Annotation {
	var out []Annotation
	name := baseName(req.Name)
	lowerName := strings.ToLower(name)
	lowerPkg := strings.ToLower(req.PkgName)

	// 寫入類操作：要求登入
	if req.Kind == "method" || req.Kind == "func" {
		if hasAnyPrefix(name, "Delete", "Update", "Create", "Remove", "Save", "Insert") {
			out = append(out, Annotation{Type: Security, Value: "requires_auth"})
		}
	}

	// admin 命名空間
	if strings.HasPrefix(name, "Admin") || strings.Contains(lowerPkg, "admin") {
		out = append(out, Annotation{Type: Security, Value: "admin_only"})
	}

	// 參數中含 id → constraint
	for _, p := range req.Params {
		if strings.EqualFold(p, "id") || strings.HasSuffix(strings.ToLower(p), "id") {
			out = append(out, Annotation{Type: Constraint, Value: "id_required"})
			break
		}
	}

	// 列表類操作 → 預設分頁
	if strings.Contains(lowerName, "list") || strings.Contains(lowerName, "index") || strings.Contains(lowerName, "search") {
		out = append(out, Annotation{Type: Constraint, Value: "paginated max_items=100"})
	}

	// 永遠補上 owner 提示
	out = append(out, Annotation{Type: Owner, Value: "team=unknown"})
	return out
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// baseName 移除 receiver 前綴，回傳純名稱
func baseName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
