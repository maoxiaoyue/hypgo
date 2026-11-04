package impact

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// RouteImpact 受影響的路由
type RouteImpact struct {
	Route  string `json:"route"`  // "POST /api/users" 或 "grpc UserService/Create"
	Direct bool   `json:"direct"` // true：handler 或宣告的型別就在這個檔案
	Reason string `json:"reason"`
}

// manifestRoutes 只讀 .hyp/context.yaml 需要的欄位：不 import pkg/manifest，impact 維持輕量
type manifestRoutes struct {
	Routes []struct {
		Protocol      string         `yaml:"protocol"`
		Command       string         `yaml:"command"`
		Method        string         `yaml:"method"`
		Path          string         `yaml:"path"`
		HandlerNames  []string       `yaml:"handler_names"`
		InputType     string         `yaml:"input_type"`
		OutputType    string         `yaml:"output_type"`
		ResponseTypes map[int]string `yaml:"response_types"`
	} `yaml:"routes"`
}

// applyRoutes 比對 manifest 找出受影響的路由。先前 help 宣稱會分析路由，實際上完全沒有
func (r *ImpactReport) applyRoutes(p *project, decls *fileDecls, reach reachability) {
	data, err := os.ReadFile(filepath.Join(p.root, ".hyp", "context.yaml"))
	if err != nil {
		return
	}
	var m manifestRoutes
	if err := yaml.Unmarshal(data, &m); err != nil {
		r.Warnings = append(r.Warnings, "cannot read .hyp/context.yaml: "+err.Error())
		return
	}
	r.RoutesKnown = true

	// 受影響的套件：handler 以 package 名稱比對（handler_names 只有最後一段路徑），
	// 註冊位置以套件路徑記錄
	affected := map[string]bool{}
	var registered []packageRoutes
	for _, pkg := range append(append([]string{r.Package}, reach.direct...), reach.transitive...) {
		info := p.packages[pkg]
		if info == nil {
			continue
		}
		if info.name != "" {
			affected[info.name] = true
		}
		if refs := scanPackageRoutes(info.files); len(refs) > 0 {
			registered = append(registered, packageRoutes{pkg: pkg, refs: refs})
		}
	}

	for _, route := range m.Routes {
		label := route.Method + " " + route.Path
		if route.Protocol != "" && route.Protocol != "rest" {
			label = route.Protocol + " " + route.Command
		}
		if decls != nil && matchesAnyRef(decls.routes, route.Method, route.Path, route.Command) {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Direct: true, Reason: "registered in this file"})
			continue
		}
		if reason := directRouteReason(decls, route.HandlerNames, route.InputType, route.OutputType, route.ResponseTypes); reason != "" {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Direct: true, Reason: reason})
			continue
		}
		if reason := indirectRouteReason(affected, registered, route.HandlerNames, route.Method, route.Path, route.Command); reason != "" {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Reason: reason})
		}
	}
	sort.SliceStable(r.Routes, func(i, j int) bool { return r.Routes[i].Direct && !r.Routes[j].Direct })
}

// indirectRouteReason 判斷路由是否經由受影響的套件：handler 或註冊位置在其中。
// 只看 handler 不夠：routers 依賴 controllers，改 routers 時 handler 所在的 controllers 不算受影響，
// 但路由正是在 routers 註冊的（改動可能換掉 handler、路徑或中間件）
func indirectRouteReason(affected map[string]bool, registered []packageRoutes, handlers []string, method, routePath, command string) string {
	for _, h := range handlers {
		if pkg, _, ok := strings.Cut(h, "."); ok && affected[pkg] {
			return "handler " + h + " is in an affected package"
		}
	}
	for _, pr := range registered {
		if matchesAnyRef(pr.refs, method, routePath, command) {
			return "registered in affected package " + pr.pkg
		}
	}
	return ""
}

// routeRef 為原始碼裡的一個路由註冊
type routeRef struct {
	method  string
	path    string
	command string // 非 REST（schema.RegisterGRPC 等）
}

// packageRoutes 為套件內的路由註冊
type packageRoutes struct {
	pkg  string
	refs []routeRef
}

var httpVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true,
}

var schemaRegisters = map[string]bool{"RegisterGRPC": true, "RegisterCLI": true, "RegisterDesktop": true}

// scanPackageRoutes 掃描多個檔案的路由註冊
func scanPackageRoutes(files []string) []routeRef {
	var refs []routeRef
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		refs = append(refs, scanRouteRefs(f)...)
	}
	return refs
}

// scanRouteRefs 找出 schema.Route{...}、r.GET("/x", h)、schema.RegisterGRPC("Svc/M", ...)。
// 只處理字串常值：路徑由變數組成時無法靜態判斷
func scanRouteRefs(f *ast.File) []routeRef {
	var refs []routeRef
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if isSchemaSelector(n.Type, "Route") {
				if ref, ok := routeFromLiteral(n); ok {
					refs = append(refs, ref)
				}
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || len(n.Args) == 0 {
				return true
			}
			arg, ok := stringLiteral(n.Args[0])
			if !ok {
				return true
			}
			if httpVerbs[sel.Sel.Name] {
				refs = append(refs, routeRef{method: sel.Sel.Name, path: arg})
			} else if schemaRegisters[sel.Sel.Name] && isSchemaSelector(sel, sel.Sel.Name) {
				refs = append(refs, routeRef{command: arg})
			}
		}
		return true
	})
	return refs
}

func isSchemaSelector(expr ast.Expr, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "schema"
}

func routeFromLiteral(lit *ast.CompositeLit) (routeRef, bool) {
	var ref routeRef
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		value, ok := stringLiteral(kv.Value)
		if !ok {
			continue
		}
		switch key.Name {
		case "Method":
			ref.method = strings.ToUpper(value)
		case "Path":
			ref.path = value
		case "Command":
			ref.command = value
		}
	}
	return ref, ref.command != "" || (ref.method != "" && ref.path != "")
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// matchesAnyRef 比對 manifest 的路由與原始碼的註冊。group 底下的路徑不含前綴，所以比對結尾
func matchesAnyRef(refs []routeRef, method, routePath, command string) bool {
	for _, ref := range refs {
		if ref.command != "" {
			if ref.command == command {
				return true
			}
			continue
		}
		if ref.method != method {
			continue
		}
		if ref.path == routePath {
			return true
		}
		if trimmed := strings.Trim(ref.path, "/"); trimmed != "" && strings.HasSuffix(routePath, "/"+trimmed) {
			return true
		}
	}
	return false
}

// directRouteReason 判斷路由是否直接宣告在目標檔案上：handler 在這裡，或 Input / Output / Responses 的型別在這裡
func directRouteReason(decls *fileDecls, handlers []string, input, output string, responses map[int]string) string {
	if decls == nil {
		return ""
	}
	for _, h := range handlers {
		for _, fn := range decls.handlers {
			if h == fn || strings.HasPrefix(h, fn+".func") { // 閉包：routers.Setup.func1
				return "handler " + h
			}
		}
	}
	if decls.declaresType(input) {
		return "input type " + input
	}
	if decls.declaresType(output) {
		return "output type " + output
	}
	codes := make([]int, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	for _, code := range codes {
		if decls.declaresType(responses[code]) {
			return fmt.Sprintf("response %d type %s", code, responses[code])
		}
	}
	return ""
}

// declaresType 回報 manifest 的型別名稱（如 "[]UserResp"、"map[string]UserResp"）是否宣告在這個檔案
func (d *fileDecls) declaresType(name string) bool {
	name = strings.TrimLeft(name, "[]*")
	if i := strings.LastIndex(name, "]"); i >= 0 {
		name = strings.TrimLeft(name[i+1:], "*")
	}
	return name != "" && d.symbols[name] == kindType
}

// directRouteCount 回傳直接受影響的路由數
func (r *ImpactReport) directRouteCount() int {
	n := 0
	for _, route := range r.Routes {
		if route.Direct {
			n++
		}
	}
	return n
}
