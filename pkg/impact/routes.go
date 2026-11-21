package impact

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// RouteImpact 受影響的路由
type RouteImpact struct {
	Route  string `json:"route"`  // "POST /api/users" 或 "grpc UserService/Create"
	Direct bool   `json:"direct"` // true：handler、註冊位置或宣告的型別就在這個檔案
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
	manifest := filepath.Join(p.root, ".hyp", "context.yaml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return
	}
	var m manifestRoutes
	if err := yaml.Unmarshal(data, &m); err != nil {
		r.Warnings = append(r.Warnings, "cannot read .hyp/context.yaml: "+err.Error())
		return
	}
	r.RoutesKnown = true
	// 先前不比對時間：原始碼改過之後 manifest 裡仍是舊的路由（含已刪除的），報告照列而沒有提示
	if info, err := os.Stat(manifest); err == nil && p.newest.After(info.ModTime()) {
		r.Warnings = append(r.Warnings, fmt.Sprintf(".hyp/context.yaml is older than %s: the routes below may be out of date, run hyp context to refresh it", p.newestFile))
	}

	// 受影響的套件：handler 以 package 名稱比對（handler_names 只有最後一段路徑），
	// 並且要真的宣告在其中；註冊位置以套件路徑記錄
	affected := map[string]map[string]bool{} // package 名稱 → 宣告的函式（runtime 名稱）
	var registered []packageRoutes
	for _, pkg := range append(append([]string{r.Package}, reach.direct...), reach.transitive...) {
		info := p.packages[pkg]
		if info == nil {
			continue
		}
		refs, funcs := scanPackage(info.files)
		if info.name != "" {
			if affected[info.name] == nil {
				affected[info.name] = map[string]bool{}
			}
			for _, fn := range funcs {
				affected[info.name][fn] = true
			}
		}
		if len(refs) > 0 {
			registered = append(registered, packageRoutes{pkg: pkg, refs: refs})
		}
	}

	for _, route := range m.Routes {
		label := route.Method + " " + route.Path
		if route.Protocol != "" && route.Protocol != "rest" {
			label = route.Protocol + " " + route.Command
		}
		here := matchNone
		if decls != nil {
			here = matchRefs(decls.routes, route.Method, route.Path, route.Command, route.HandlerNames)
		}
		if here == matchSure {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Direct: true, Reason: "registered in this file"})
			continue
		}
		if reason := directRouteReason(decls, route.HandlerNames, route.InputType, route.OutputType, route.ResponseTypes); reason != "" {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Direct: true, Reason: reason})
			continue
		}
		if here == matchMaybe {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Reason: "possibly registered in this file (only the end of the path matches)"})
			continue
		}
		if reason := indirectRouteReason(affected, registered, route.HandlerNames, route.Method, route.Path, route.Command); reason != "" {
			r.Routes = append(r.Routes, RouteImpact{Route: label, Reason: reason})
		}
	}
	sort.SliceStable(r.Routes, func(i, j int) bool { return r.Routes[i].Direct && !r.Routes[j].Direct })
}

// closureSuffix 比對 runtime 名稱裡閉包與泛型實例的尾巴：routers.Setup.func1.2、pkg.F[...]
var closureSuffix = regexp.MustCompile(`(\[[^\]]*\])?(\.func\d+(\.\d+)*)*(-fm)?$`)

// handlerFunc 把 manifest 的 handler 名稱還原成宣告它的函式（去掉閉包、泛型、method value 的尾巴）
func handlerFunc(h string) string { return closureSuffix.ReplaceAllString(h, "") }

// indirectRouteReason 判斷路由是否經由受影響的套件：handler 或註冊位置在其中。
// 只看 handler 不夠：routers 依賴 controllers，改 routers 時 handler 所在的 controllers 不算受影響，
// 但路由正是在 routers 註冊的（改動可能換掉 handler、路徑或中間件）。
// handler 除了 package 名稱相同，還要真的宣告在受影響的套件裡：先前同名的套件
// （app/controllers 與 app/admin/controllers）互相把對方的路由全部算成受影響
func indirectRouteReason(affected map[string]map[string]bool, registered []packageRoutes, handlers []string, method, routePath, command string) string {
	for _, h := range handlers {
		if pkg, _, ok := strings.Cut(h, "."); ok && affected[pkg][handlerFunc(h)] {
			return "handler " + h + " is in an affected package"
		}
	}
	for _, pr := range registered {
		if matchRefs(pr.refs, method, routePath, command, handlers) != matchNone {
			return "registered in affected package " + pr.pkg
		}
	}
	return ""
}

// routeRef 為原始碼裡的一個路由註冊
type routeRef struct {
	method  string
	path    string // 已接上同一個函式內追得到的 Group 前綴
	grouped bool   // path 含 Group 前綴
	handler string // handler 的函式名（c.Get → "Get"）；閉包或看不出來時為空
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

// scanPackage 掃描套件的檔案，回傳路由註冊與宣告的函式（runtime 名稱）
func scanPackage(files []string) (refs []routeRef, funcs []string) {
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		refs = append(refs, scanRouteRefs(f)...)
		funcs = append(funcs, funcNames(f)...)
	}
	return refs, funcs
}

// funcNames 回傳檔案內所有函式與方法在 runtime 的名稱（pkg.F、pkg.(*T).M、pkg.T.M）
func funcNames(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			out = append(out, f.Name.Name+"."+fn.Name.Name)
			continue
		}
		recv, pointer := receiverName(fn.Recv.List[0].Type)
		if pointer {
			out = append(out, f.Name.Name+".(*"+recv+")."+fn.Name.Name)
		} else {
			out = append(out, f.Name.Name+"."+recv+"."+fn.Name.Name)
		}
	}
	return out
}

// scanRouteRefs 找出 schema.Route{...}、r.GET("/x", h)、schema.RegisterGRPC("Svc/M", ...)。
// 只處理字串常值：路徑由變數組成時無法靜態判斷。
// 每個函式內追蹤 g := r.Group("/api/users")，把前綴接到 g.GET("/:id", h) 的路徑上
func scanRouteRefs(f *ast.File) []routeRef {
	var refs []routeRef
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			refs = append(refs, scanFuncRoutes(fn.Body)...)
		} else if gen, ok := decl.(*ast.GenDecl); ok {
			refs = append(refs, scanFuncRoutes(gen)...) // 套件層級的 var routes = []schema.Route{...}
		}
	}
	return refs
}

// groupPrefix 為變數的 Group 前綴：base 是它從哪個變數的 Group 得來
type groupPrefix struct {
	base   string
	prefix string
}

func scanFuncRoutes(body ast.Node) []routeRef {
	// 第一輪：x := y.Group("/p")（含 x = ...、var x = ...）
	groups := map[string]groupPrefix{}
	record := func(lhs, rhs ast.Expr) {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return
		}
		if base, prefix, ok := groupCall(rhs); ok {
			groups[id.Name] = groupPrefix{base: base, prefix: prefix}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i := range n.Lhs {
					record(n.Lhs[i], n.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i := range n.Names {
					record(n.Names[i], n.Values[i])
				}
			}
		}
		return true
	})
	// prefixOf 回傳接收者累積的前綴；接收者不是 Group 得來的變數時 grouped 為 false
	var prefixOf func(expr ast.Expr, depth int) (string, bool)
	prefixOf = func(expr ast.Expr, depth int) (string, bool) {
		if base, prefix, ok := groupCall(expr); ok { // r.Group("/a").GET(...)
			parent, _ := prefixOf(&ast.Ident{Name: base}, depth+1)
			return joinRoute(parent, prefix), true
		}
		id, ok := expr.(*ast.Ident)
		if !ok || depth > 8 {
			return "", false
		}
		g, ok := groups[id.Name]
		if !ok || g.base == id.Name {
			return "", false
		}
		parent, _ := prefixOf(&ast.Ident{Name: g.base}, depth+1)
		return joinRoute(parent, g.prefix), true
	}

	var refs []routeRef
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			// 不在呼叫裡的 schema.Route{...}（表格式註冊、變數）
			if lit, ok := n.(*ast.CompositeLit); ok && isSchemaSelector(lit.Type, "Route") {
				if ref, ok := routeFromLiteral(lit); ok {
					refs = append(refs, ref)
				}
				return false
			}
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		switch {
		case sel.Sel.Name == "Handle" && len(call.Args) == 1:
			// g.Schema(schema.Route{...}).Handle(h)
			inner, ok := sel.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			innerSel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || innerSel.Sel.Name != "Schema" || len(inner.Args) != 1 {
				return true
			}
			lit, ok := inner.Args[0].(*ast.CompositeLit)
			if !ok || !isSchemaSelector(lit.Type, "Route") {
				return true
			}
			if ref, ok := routeFromLiteral(lit); ok {
				ref.handler = handlerName(call.Args[0])
				if prefix, grouped := prefixOf(innerSel.X, 0); grouped && ref.command == "" {
					ref.path, ref.grouped = joinRoute(prefix, ref.path), true
				}
				refs = append(refs, ref)
			}
			return false
		case httpVerbs[sel.Sel.Name] && len(call.Args) >= 2:
			// 至少要有路徑與 handler：先前 HTTP 客戶端的 client.GET("/x") 也被當成路由註冊
			if arg, ok := stringLiteral(call.Args[0]); ok {
				ref := routeRef{method: sel.Sel.Name, path: arg, handler: handlerName(call.Args[len(call.Args)-1])}
				if prefix, grouped := prefixOf(sel.X, 0); grouped {
					ref.path, ref.grouped = joinRoute(prefix, arg), true
				}
				refs = append(refs, ref)
			}
		case schemaRegisters[sel.Sel.Name] && isSchemaSelector(sel, sel.Sel.Name):
			if arg, ok := stringLiteral(call.Args[0]); ok {
				refs = append(refs, routeRef{command: arg})
			}
		}
		return true
	})
	return refs
}

// groupCall 辨認 x.Group("/prefix", ...)，回傳 x 的名稱與前綴
func groupCall(expr ast.Expr) (base, prefix string, ok bool) {
	call, isCall := expr.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return "", "", false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Group" {
		return "", "", false
	}
	prefix, isLit := stringLiteral(call.Args[0])
	if !isLit {
		return "", "", false
	}
	if id, isIdent := sel.X.(*ast.Ident); isIdent {
		base = id.Name
	}
	return base, prefix, true
}

// handlerName 取出 handler 的函式名：controllers.List → List、c.Get → Get；閉包、呼叫、區域變數回傳 ""
func handlerName(expr ast.Expr) string {
	// 只認 pkg.Func / recv.Method：單獨的識別字可能是區域變數（h := wrap(...)），名稱不代表 handler
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

// joinRoute 接上 group 前綴與路徑，中間只留一個斜線
func joinRoute(prefix, p string) string {
	joined := strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(p, "/")
	if joined != "/" {
		joined = strings.TrimRight(joined, "/")
	}
	return joined
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

// 路由與原始碼註冊的比對結果
const (
	matchNone  = iota
	matchMaybe // 只有路徑結尾相同，沒有 Group 前綴或 handler 可以佐證
	matchSure
)

// matchRefs 比對 manifest 的路由與原始碼的註冊，回傳最確定的結果。
//
// 路徑完全相同即確定。否則比對結尾（註冊的那一層上面可能還有呼叫端傳進來的 group）：
// 先前只要結尾相同就算，routers/order.go 的 g.GET("/:id") 把 GET /api/users/:id 也認成自己註冊的。
// 現在註冊的 handler 看得出名稱時必須與 manifest 的 handler 相符；
// 結尾相同而有 Group 前綴或 handler 佐證才算確定，否則只是「可能」
func matchRefs(refs []routeRef, method, routePath, command string, handlers []string) int {
	best := matchNone
	for _, ref := range refs {
		if ref.command != "" {
			if ref.command == command {
				return matchSure
			}
			continue
		}
		if ref.method != method {
			continue
		}
		confirmed := false
		if ref.handler != "" && len(handlers) > 0 {
			for _, h := range handlers {
				fn := handlerFunc(h)
				if fn[strings.LastIndex(fn, ".")+1:] == ref.handler {
					confirmed = true
				}
			}
			if !confirmed {
				continue // 同一個路徑結尾，但 handler 是別人的
			}
		}
		if ref.path == routePath {
			return matchSure
		}
		trimmed := strings.Trim(ref.path, "/")
		if trimmed == "" || !strings.HasSuffix(routePath, "/"+trimmed) {
			continue
		}
		if ref.grouped || confirmed {
			return matchSure
		}
		best = matchMaybe
	}
	return best
}

// directRouteReason 判斷路由是否直接宣告在目標檔案上：handler 在這裡，或 Input / Output / Responses 的型別在這裡
func directRouteReason(decls *fileDecls, handlers []string, input, output string, responses map[int]string) string {
	if decls == nil {
		return ""
	}
	for _, h := range handlers {
		for _, fn := range decls.handlers {
			if handlerFunc(h) == fn {
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

// typeNames 比對型別表示式裡的每個具名型別（可帶套件名）
var typeNames = regexp.MustCompile(`(?:([A-Za-z_]\w*)\.)?([A-Za-z_]\w*)`)

// declaresType 回報 manifest 的型別名稱是否宣告在這個檔案。
// manifest 的名稱帶套件名（"models.UserResp"、"[]models.UserResp"、"api.Page[models.User]"），
// 套件名要等於這個檔案的 package；沒帶套件名（舊的 manifest）時只比對型別名稱。
// 先前拿整個字串去比對不帶套件名的符號：型別名稱改為帶套件名之後，直接宣告的路由一條也比不到
func (d *fileDecls) declaresType(name string) bool {
	for _, m := range typeNames.FindAllStringSubmatch(name, -1) {
		if (m[1] == "" || m[1] == d.pkgName) && d.symbols[m[2]] == kindType {
			return true
		}
	}
	return false
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
