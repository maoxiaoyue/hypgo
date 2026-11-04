package impact

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// 匯出符號的種類
const (
	kindFunc   = "func"
	kindType   = "type"
	kindConst  = "const"
	kindVar    = "var"
	kindMethod = "method"
)

// SymbolUsage 目標檔案的一個匯出符號，以及其他套件的使用情況
type SymbolUsage struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`               // func / type / const / var / method
	Methods  []string `json:"methods,omitempty"`  // type 在這個檔案宣告的匯出方法
	Uses     int      `json:"uses"`               // 其他套件的引用次數
	Packages []string `json:"packages,omitempty"` // 引用它的套件
	Tracked  bool     `json:"tracked"`            // false：方法呼叫需要型別資訊，AST 無法追蹤
}

// fileDecls 為目標檔案的宣告
type fileDecls struct {
	pkgName     string
	symbols     map[string]string   // 套件層級的匯出符號 → 種類
	methods     map[string][]string // 匯出型別 → 匯出方法
	handlers    []string            // 所有函式與方法在 runtime 的名稱（比對 manifest 的 handler_names）
	hasBunModel bool                // 有 struct 內嵌 bun.BaseModel
	routes      []routeRef          // 在這個檔案註冊的路由
}

// parseFileDecls 解析目標檔案的匯出 API
func parseFileDecls(file string) (*fileDecls, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	d := &fileDecls{pkgName: f.Name.Name, symbols: map[string]string{}, methods: map[string][]string{}}
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			d.addFunc(decl)
		case *ast.GenDecl:
			d.addGen(decl)
		}
	}
	for _, m := range d.methods {
		sort.Strings(m)
	}
	d.routes = scanRouteRefs(f)
	return d, nil
}

func (d *fileDecls) addFunc(fn *ast.FuncDecl) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		d.handlers = append(d.handlers, d.pkgName+"."+fn.Name.Name)
		if fn.Name.IsExported() {
			d.symbols[fn.Name.Name] = kindFunc
		}
		return
	}
	recv, pointer := receiverName(fn.Recv.List[0].Type)
	if pointer {
		d.handlers = append(d.handlers, d.pkgName+".(*"+recv+")."+fn.Name.Name)
	} else {
		d.handlers = append(d.handlers, d.pkgName+"."+recv+"."+fn.Name.Name)
	}
	if fn.Name.IsExported() && ast.IsExported(recv) {
		d.methods[recv] = append(d.methods[recv], fn.Name.Name)
	}
}

func (d *fileDecls) addGen(gen *ast.GenDecl) {
	for _, spec := range gen.Specs {
		switch spec := spec.(type) {
		case *ast.TypeSpec:
			if spec.Name.IsExported() {
				d.symbols[spec.Name.Name] = kindType
			}
			if st, ok := spec.Type.(*ast.StructType); ok && embedsBunModel(st) {
				d.hasBunModel = true
			}
		case *ast.ValueSpec:
			kind := kindVar
			if gen.Tok == token.CONST {
				kind = kindConst
			}
			for _, name := range spec.Names {
				if name.IsExported() {
					d.symbols[name.Name] = kind
				}
			}
		}
	}
}

// receiverName 取出 method receiver 的型別名稱（含泛型 receiver）
func receiverName(expr ast.Expr) (name string, pointer bool) {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr, pointer = star.X, true
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, pointer
	case *ast.IndexExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name, pointer
		}
	case *ast.IndexListExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name, pointer
		}
	}
	return "?", pointer
}

func embedsBunModel(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "BaseModel" {
			continue
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == "bun" {
			return true
		}
	}
	return false
}

// usageIndex 記錄其他套件引用目標檔案匯出符號的情況
type usageIndex struct {
	counts   map[string]int             // 符號 → 引用次數
	packages map[string]map[string]bool // 符號 → 引用它的套件
	byPkg    map[string]map[string]bool // 套件 → 它用到的符號
}

// scanUsage 掃描 import 目標套件的所有檔案，統計 pkg.Symbol 的引用。
// 這只追蹤「套件層級的符號」：方法呼叫（x.With()）需要型別資訊才能判斷，hyp lint --deep 那種
// 載入完整型別的做法會讓分析變慢，也要求專案能編譯，所以這裡刻意不做並在報告中標明
func (p *project) scanUsage(target string, decls *fileDecls, report *ImpactReport) *usageIndex {
	u := &usageIndex{counts: map[string]int{}, packages: map[string]map[string]bool{}, byPkg: map[string]map[string]bool{}}
	if decls == nil || p.packages[target] == nil {
		return u
	}
	for _, pkg := range sortedPackages(p.packages) {
		info := p.packages[pkg]
		if pkg == target || !(info.imports[target] || info.testImports[target]) {
			continue
		}
		for _, file := range append(append([]string(nil), info.files...), info.testFiles...) {
			u.scanFile(file, pkg, target, decls, report, p)
		}
	}
	return u
}

func (u *usageIndex) scanFile(file, pkg, target string, decls *fileDecls, report *ImpactReport, p *project) {
	// 這裡要 object resolution：X.Obj 非 nil 代表區域變數遮蔽了套件名，不是對套件的引用
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		return
	}
	local := ""
	for _, imp := range f.Imports {
		if p.resolveImport(strings.Trim(imp.Path.Value, `"`)) != target {
			continue
		}
		local = decls.pkgName
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	switch local {
	case "", "_":
		return
	case ".":
		report.Warnings = append(report.Warnings, pkg+" dot-imports this package: its usage is not tracked")
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != local || x.Obj != nil {
			return true
		}
		if _, exported := decls.symbols[sel.Sel.Name]; exported {
			u.record(sel.Sel.Name, pkg)
		}
		return true
	})
}

func (u *usageIndex) record(symbol, pkg string) {
	u.counts[symbol]++
	if u.packages[symbol] == nil {
		u.packages[symbol] = map[string]bool{}
	}
	u.packages[symbol][pkg] = true
	if u.byPkg[pkg] == nil {
		u.byPkg[pkg] = map[string]bool{}
	}
	u.byPkg[pkg][symbol] = true
}

func (u *usageIndex) usedBy(pkg string) []string {
	if len(u.byPkg[pkg]) == 0 {
		return nil
	}
	return sortedKeys(u.byPkg[pkg])
}

// symbols 組出報告的 API 區段：外部引用多的在前
func (u *usageIndex) symbols(decls *fileDecls) []SymbolUsage {
	if decls == nil {
		return nil
	}
	out := make([]SymbolUsage, 0, len(decls.symbols))
	for name, kind := range decls.symbols {
		s := SymbolUsage{Name: name, Kind: kind, Uses: u.counts[name], Tracked: true}
		if u.packages[name] != nil {
			s.Packages = sortedKeys(u.packages[name])
		}
		if kind == kindType {
			s.Methods = decls.methods[name]
		}
		out = append(out, s)
	}
	// 方法的型別宣告在其他檔案：這個檔案的改動仍會影響它，但無法追蹤呼叫者
	for typ, methods := range decls.methods {
		if decls.symbols[typ] == kindType {
			continue
		}
		for _, m := range methods {
			out = append(out, SymbolUsage{Name: typ + "." + m, Kind: kindMethod})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].Name < out[j].Name
	})
	return out
}
