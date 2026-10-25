package testgen

// render.go — 依函式簽名產生測試骨架：一般函式 / 方法產生 table-driven 骨架，
// HypGo handler 與路由註冊函式改以 contract 測試驗證（仍為純 AST 分析，不載入型別資訊）。

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

const (
	hypContextPath  = "github.com/maoxiaoyue/hypgo/pkg/context"
	hypRouterPath   = "github.com/maoxiaoyue/hypgo/pkg/router"
	hypContractPath = "github.com/maoxiaoyue/hypgo/pkg/contract"
)

// candKind 決定一個候選函式產生什麼樣的測試
type candKind int

const (
	kindStub      candKind = iota // 只含 t.Skip 的空殼（無法依簽名產生骨架時）
	kindTable                     // 依簽名產生的 table-driven 骨架
	kindHandler                   // HypGo handler func(*hypcontext.Context)：由 contract 測試涵蓋
	kindRegistrar                 // 路由註冊函式 func(*router.Router)：根註冊函式產生 contract 測試
)

// importInfo 原始檔一個 import 的引用名稱與 spec 文字（`"time"` 或 `m "x/models"`）
type importInfo struct {
	path string
	spec string
}

// fileImports 以引用名稱索引原始檔的 import。未取別名時以路徑推測套件名
// （去掉 /vN、.vN 與 go- 前綴）；推測不到的名稱在 tableSpec 中查不到，退回一般空殼
func fileImports(f *ast.File) map[string]importInfo {
	out := map[string]importInfo{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name != "_" && imp.Name.Name != "." {
				out[imp.Name.Name] = importInfo{path: path, spec: imp.Name.Name + " " + imp.Path.Value}
			}
			continue
		}
		out[guessPkgName(path)] = importInfo{path: path, spec: imp.Path.Value}
	}
	return out
}

func guessPkgName(path string) string {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	if len(parts) > 1 && len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		name = parts[len(parts)-2] // github.com/redis/go-redis/v9 → go-redis
	}
	if i := strings.Index(name, ".v"); i > 0 {
		name = name[:i] // gopkg.in/yaml.v3 → yaml
	}
	return strings.TrimPrefix(name, "go-")
}

// importNameOf 回傳 path 在檔案中的引用名稱；未 import 時回傳空字串
func importNameOf(imps map[string]importInfo, path string) string {
	for name, imp := range imps {
		if imp.path == path {
			return name
		}
	}
	return ""
}

// singleParamOf 回報 fd 是否恰好一個 *pkg.Type 參數、沒有回傳值
func singleParamOf(fd *ast.FuncDecl, pkg, typeName string) bool {
	if pkg == "" || fd.Type.Results != nil || len(fd.Type.Params.List) != 1 || len(fd.Type.Params.List[0].Names) > 1 {
		return false
	}
	star, ok := fd.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg && sel.Sel.Name == typeName
}

// classify 決定候選函式的測試種類
func classify(fd *ast.FuncDecl, imps map[string]importInfo) candKind {
	if singleParamOf(fd, importNameOf(imps, hypContextPath), "Context") {
		return kindHandler
	}
	if singleParamOf(fd, importNameOf(imps, hypRouterPath), "Router") {
		return kindRegistrar
	}
	if newTableSpec(fd, imps) != nil {
		return kindTable
	}
	return kindStub
}

// ---------- table-driven 骨架 ----------

type tableField struct {
	name, typ string
	cmp       bool // 以 reflect.DeepEqual 比對；func 值無法比對（只有都為 nil 才相等），不產生 want 欄位
}

// tableSpec 依簽名組出的 table-driven 測試內容
type tableSpec struct {
	recv     *tableField
	params   []tableField
	variadic bool
	results  []tableField // 非 error 的回傳值：want、want1…
	hasErr   bool         // 最後一個回傳值為 error
	imports  []string     // 簽名用到的 import spec
}

// reservedNames 測試本身使用的識別字，參數同名時加上 Arg 後綴
var reservedNames = map[string]bool{
	"t": true, "tt": true, "tests": true, "name": true, "recv": true,
	"err": true, "wantErr": true, "reflect": true, "testing": true,
}

// newTableSpec 依函式簽名建立骨架內容；泛型函式、泛型 receiver 或簽名引用了解析不到的套件時回傳 nil
func newTableSpec(fd *ast.FuncDecl, imps map[string]importInfo) *tableSpec {
	if fd.Type.TypeParams != nil {
		return nil
	}
	spec := &tableSpec{}
	needed := map[string]bool{}
	ok := true
	collect := func(e ast.Expr) {
		ast.Inspect(e, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.IndexExpr, *ast.IndexListExpr:
				ok = false // 泛型型別實例化：測試中無法得知型別參數
			case *ast.SelectorExpr:
				if x, isIdent := v.X.(*ast.Ident); isIdent {
					imp, found := imps[x.Name]
					if !found {
						ok = false
						return false
					}
					needed[imp.spec] = true
				}
				return false
			}
			return true
		})
	}

	used := map[string]bool{}
	uniq := func(n string) string {
		if reservedNames[n] || strings.HasPrefix(n, "want") || strings.HasPrefix(n, "got") {
			n += "Arg"
		}
		for used[n] {
			n += "_"
		}
		used[n] = true
		return n
	}

	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		r := fd.Recv.List[0].Type
		collect(r)
		spec.recv = &tableField{name: "recv", typ: types.ExprString(r)}
		used["recv"] = true
	}
	idx := 0
	for _, p := range fd.Type.Params.List {
		typ := p.Type
		if ell, isEll := typ.(*ast.Ellipsis); isEll {
			spec.variadic = true
			typ = &ast.ArrayType{Elt: ell.Elt}
		}
		collect(typ)
		names := p.Names
		if len(names) == 0 {
			names = []*ast.Ident{nil}
		}
		for _, n := range names {
			name := fmt.Sprintf("arg%d", idx)
			if n != nil && n.Name != "_" {
				name = n.Name
			}
			spec.params = append(spec.params, tableField{name: uniq(name), typ: types.ExprString(typ)})
			idx++
		}
	}
	if fd.Type.Results != nil {
		var results []ast.Expr
		for _, r := range fd.Type.Results.List {
			for range max(1, len(r.Names)) {
				results = append(results, r.Type)
			}
		}
		if last := len(results) - 1; last >= 0 {
			if id, isIdent := results[last].(*ast.Ident); isIdent && id.Name == "error" {
				spec.hasErr = true
				results = results[:last]
			}
		}
		for i, r := range results {
			collect(r)
			name := "want"
			if i > 0 {
				name = fmt.Sprintf("want%d", i)
			}
			_, isFunc := r.(*ast.FuncType)
			spec.results = append(spec.results, tableField{name: name, typ: types.ExprString(r), cmp: !isFunc})
		}
	}
	if !ok {
		return nil
	}
	for s := range needed {
		spec.imports = append(spec.imports, s)
	}
	sort.Strings(spec.imports)
	return spec
}

// needsReflect 回報骨架是否以 reflect.DeepEqual 比對回傳值
func (s *tableSpec) needsReflect() bool {
	for _, r := range s.results {
		if r.cmp {
			return true
		}
	}
	return false
}

// render 產生 table-driven 測試函式
func (s *tableSpec) render(c candidate, msg string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func %s(t *testing.T) {\n\tt.Skip(%q)\n\ttests := []struct {\n\t\tname string\n", c.testName, msg)
	if s.recv != nil {
		fmt.Fprintf(&b, "\t\t%s %s\n", s.recv.name, s.recv.typ)
	}
	for _, f := range s.params {
		fmt.Fprintf(&b, "\t\t%s %s\n", f.name, f.typ)
	}
	for _, f := range s.results {
		if f.cmp {
			fmt.Fprintf(&b, "\t\t%s %s\n", f.name, f.typ)
		}
	}
	if s.hasErr {
		b.WriteString("\t\twantErr bool\n")
	}
	b.WriteString("\t}{\n\t\t// TODO: add test cases\n\t}\n\tfor _, tt := range tests {\n\t\tt.Run(tt.name, func(t *testing.T) {\n\t\t\t")

	args := make([]string, len(s.params))
	for i, p := range s.params {
		args[i] = "tt." + p.name
	}
	if s.variadic && len(args) > 0 {
		args[len(args)-1] += "..."
	}
	call := c.funcName + "(" + strings.Join(args, ", ") + ")"
	if s.recv != nil {
		call = "tt.recv." + call
	}
	var lhs []string
	for i := range s.results {
		if i == 0 {
			lhs = append(lhs, "got")
		} else {
			lhs = append(lhs, fmt.Sprintf("got%d", i))
		}
	}
	if s.hasErr {
		lhs = append(lhs, "err")
	}
	if len(lhs) > 0 {
		b.WriteString(strings.Join(lhs, ", ") + " := ")
	}
	b.WriteString(call + "\n")
	if s.hasErr {
		fmt.Fprintf(&b, "\t\t\tif (err != nil) != tt.wantErr {\n\t\t\t\tt.Fatalf(\"%s() error = %%v, wantErr %%v\", err, tt.wantErr)\n\t\t\t}\n", c.funcName)
	}
	for i, r := range s.results {
		if !r.cmp {
			fmt.Fprintf(&b, "\t\t\t_ = %s // TODO: func value: call it and check the result\n", lhs[i])
			continue
		}
		fmt.Fprintf(&b, "\t\t\tif !reflect.DeepEqual(%s, tt.%s) {\n\t\t\t\tt.Errorf(\"%s() %s = %%v, want %%v\", %s, tt.%s)\n\t\t\t}\n",
			lhs[i], r.name, c.funcName, lhs[i], lhs[i], r.name)
	}
	b.WriteString("\t\t})\n\t}\n}\n")
	return b.String()
}

// ---------- contract 測試 ----------

// renderContract 產生以 contract.TestAll 驗證根註冊函式全部路由的測試。
// 預設 t.Skip：需要授權（JWT 等）或外部服務的路由開箱會失敗，接好之後再移除
func renderContract(c candidate) string {
	return fmt.Sprintf(`// %[1]s 以 contract.TestAll 依 Schema 宣告驗證 %[2]s 註冊的全部路由。
// 需要授權或外部服務（DB…）的路由請先準備好（例如以 contract.Test 搭配 TestCase.Headers 帶 token），再移除 t.Skip
func %[1]s(t *testing.T) {
	t.Skip("auto-generated contract test; set up auth / external services, then remove this line")
	r := router.New()
	%[2]s(r)
	contract.TestAll(t, r)
}
`, c.testName, c.funcName)
}

// ---------- 寫入 ----------

// formatDecl 以 gofmt 排版一段宣告；失敗時原樣回傳（仍是合法 Go，只是未對齊）
func formatDecl(src string) string {
	if out, err := format.Source([]byte(src)); err == nil {
		return string(out)
	}
	return src
}

// addImports 把缺少的 import 以文字插入既有原始檔，不重新排版使用者的檔案
func addImports(src []byte, specs []string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, imp := range f.Imports {
		have[imp.Path.Value] = true
	}
	var missing []string
	for _, s := range specs {
		if !have[s[strings.LastIndex(s, " ")+1:]] {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return src, nil
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	var lines strings.Builder
	for _, s := range missing {
		lines.WriteString("\t" + s + "\n")
	}

	for _, d := range f.Decls {
		gd, isGen := d.(*ast.GenDecl)
		if !isGen || gd.Tok != token.IMPORT {
			continue
		}
		if gd.Lparen.IsValid() {
			at := offset(gd.Rparen)
			return append(append(append([]byte{}, src[:at]...), lines.String()...), src[at:]...), nil
		}
		// import "x" → import ( "x" ... )
		start, end := offset(gd.Pos()), offset(gd.End())
		block := "import (\n\t" + strings.TrimSpace(string(src[offset(gd.Specs[0].Pos()):end])) + "\n" + lines.String() + ")"
		return append(append(append([]byte{}, src[:start]...), block...), src[end:]...), nil
	}
	at := offset(f.Name.End())
	block := "\n\nimport (\n" + lines.String() + ")"
	return append(append(append([]byte{}, src[:at]...), block...), src[at:]...), nil
}
