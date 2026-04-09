package annotation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CheckResult 單一區塊的檢查結果
type CheckResult struct {
	Name      string // "func Create" 或 "type User"
	Kind      string // "package", "type", "func", "method", "const", "var"
	Line      int    // 宣告所在行號
	HasDoc    bool   // 是否已有文檔註解
	Suggested string // 建議的註解文字（當 HasDoc 為 false）
}

// CheckReport 完整檢查報告
type CheckReport struct {
	Filename string
	Results  []CheckResult
	Total    int // 總區塊數
	Passed   int // 已有註解的區塊數
}

// CheckFile 掃描 Go 檔案，回傳所有 exported 區塊的檢查結果
//
// 使用 go/parser 解析 AST，檢查以下 exported 宣告：
//   - package 宣告
//   - type 宣告（struct、interface）
//   - func / method 宣告
//   - const 群組
//   - var 群組
func CheckFile(filename string) (*CheckReport, error) {
	// 安全檢查：只接受 .go 檔案
	if filepath.Ext(filename) != ".go" {
		return nil, fmt.Errorf("only .go files are supported: %s", filename)
	}

	// 安全檢查：拒絕符號連結
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symbolic links are not supported: %s", filename)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	report := &CheckReport{
		Filename: filename,
	}

	// 檢查 package 宣告
	report.addResult(CheckResult{
		Name:      "package " + f.Name.Name,
		Kind:      "package",
		Line:      fset.Position(f.Package).Line,
		HasDoc:    f.Doc != nil && f.Doc.Text() != "",
		Suggested: fmt.Sprintf("// Package %s provides ...\n// @ai:owner ai-generated", f.Name.Name),
	})

	// 遍歷所有頂層宣告
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			report.checkGenDecl(fset, d)
		case *ast.FuncDecl:
			report.checkFuncDecl(fset, d)
		}
	}

	// 排序（按行號）
	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].Line < report.Results[j].Line
	})

	// 計算統計
	report.Total = len(report.Results)
	for _, r := range report.Results {
		if r.HasDoc {
			report.Passed++
		}
	}

	return report, nil
}

// checkGenDecl 檢查 type/const/var 宣告
func (r *CheckReport) checkGenDecl(fset *token.FileSet, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			hasDoc := d.Doc != nil && d.Doc.Text() != ""
			if !hasDoc && s.Doc != nil && s.Doc.Text() != "" {
				hasDoc = true
			}
			kind := "type"
			suggested := fmt.Sprintf("// %s ...\n// @ai:owner ai-generated", s.Name.Name)
			if _, ok := s.Type.(*ast.InterfaceType); ok {
				suggested = fmt.Sprintf("// %s defines ...\n// @ai:owner ai-generated", s.Name.Name)
			} else if _, ok := s.Type.(*ast.StructType); ok {
				suggested = fmt.Sprintf("// %s represents ...\n// @ai:owner ai-generated", s.Name.Name)
			}
			r.addResult(CheckResult{
				Name:      kind + " " + s.Name.Name,
				Kind:      kind,
				Line:      fset.Position(s.Pos()).Line,
				HasDoc:    hasDoc,
				Suggested: suggested,
			})

		case *ast.ValueSpec:
			for _, name := range s.Names {
				if !name.IsExported() {
					continue
				}
				kind := "var"
				if d.Tok == token.CONST {
					kind = "const"
				}
				hasDoc := d.Doc != nil && d.Doc.Text() != ""
				if !hasDoc && s.Doc != nil && s.Doc.Text() != "" {
					hasDoc = true
				}
				r.addResult(CheckResult{
					Name:      kind + " " + name.Name,
					Kind:      kind,
					Line:      fset.Position(name.Pos()).Line,
					HasDoc:    hasDoc,
					Suggested: fmt.Sprintf("// %s ...\n// @ai:owner ai-generated", name.Name),
				})
			}
		}
	}
}

// checkFuncDecl 檢查 func/method 宣告
func (r *CheckReport) checkFuncDecl(fset *token.FileSet, d *ast.FuncDecl) {
	if !d.Name.IsExported() {
		return
	}

	kind := "func"
	name := d.Name.Name
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = "method"
		recvType := exprToString(d.Recv.List[0].Type)
		name = recvType + "." + name
	}

	suggested := fmt.Sprintf("// %s ...\n// @ai:owner ai-generated", d.Name.Name)

	// 根據函式名稱推斷合適的 @ai: 標籤
	funcName := strings.ToLower(d.Name.Name)
	if strings.HasPrefix(funcName, "create") || strings.HasPrefix(funcName, "update") || strings.HasPrefix(funcName, "delete") {
		suggested = fmt.Sprintf("// %s handles %s operations.\n// @ai:owner ai-generated\n// @ai:impact routes=/api/", d.Name.Name, funcName)
	} else if strings.HasPrefix(funcName, "get") || strings.HasPrefix(funcName, "list") || strings.HasPrefix(funcName, "find") {
		suggested = fmt.Sprintf("// %s retrieves data.\n// @ai:owner ai-generated", d.Name.Name)
	} else if strings.Contains(funcName, "auth") || strings.Contains(funcName, "login") || strings.Contains(funcName, "token") {
		suggested = fmt.Sprintf("// %s handles authentication.\n// @ai:owner ai-generated\n// @ai:security requires_auth", d.Name.Name)
	}

	r.addResult(CheckResult{
		Name:      kind + " " + name,
		Kind:      kind,
		Line:      fset.Position(d.Pos()).Line,
		HasDoc:    d.Doc != nil && d.Doc.Text() != "",
		Suggested: suggested,
	})
}

// addResult 加入檢查結果
func (r *CheckReport) addResult(result CheckResult) {
	r.Results = append(r.Results, result)
}

// FixFile 為缺少註解的區塊加入建議註解
// 寫入前會先建立 .go.bak 備份
func FixFile(filename string, results []CheckResult) error {
	// 讀取原始檔案
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}

	// 建立備份
	backupPath := filename + ".bak"
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return fmt.Errorf("cannot create backup: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// 收集需要插入的行（由下往上插入，避免行號偏移）
	type insertion struct {
		line    int    // 0-based 行號（插入在此行之前）
		comment string
	}
	var insertions []insertion

	for _, r := range results {
		if r.HasDoc {
			continue
		}
		insertions = append(insertions, insertion{
			line:    r.Line - 1, // 轉為 0-based
			comment: r.Suggested,
		})
	}

	// 由下往上插入
	sort.Slice(insertions, func(i, j int) bool {
		return insertions[i].line > insertions[j].line
	})

	for _, ins := range insertions {
		if ins.line >= 0 && ins.line <= len(lines) {
			// 取得該行的縮排
			indent := ""
			if ins.line < len(lines) {
				for _, ch := range lines[ins.line] {
					if ch == '\t' || ch == ' ' {
						indent += string(ch)
					} else {
						break
					}
				}
			}

			// 處理多行註解（@ai: 標籤換行）
			commentLines := strings.Split(ins.comment, "\n")
			newLines := make([]string, len(commentLines))
			for i, cl := range commentLines {
				newLines[i] = indent + cl
			}

			// 在該行之前插入所有註解行
			before := make([]string, len(lines[:ins.line]))
			copy(before, lines[:ins.line])
			after := lines[ins.line:]
			lines = append(before, append(newLines, after...)...)
		}
	}

	return os.WriteFile(filename, []byte(strings.Join(lines, "\n")), 0644)
}

// FormatReport 格式化檢查報告為人類可讀字串
func FormatReport(report *CheckReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Scanning: %s\n\n", report.Filename))

	for _, r := range report.Results {
		if r.HasDoc {
			sb.WriteString(fmt.Sprintf("[PASS] %s — has comment\n", r.Name))
		} else {
			sb.WriteString(fmt.Sprintf("[FAIL] %s — missing comment\n", r.Name))
			sb.WriteString(fmt.Sprintf("  → suggested: %s\n", r.Suggested))
		}
	}

	sb.WriteString(fmt.Sprintf("\nSummary: %d/%d blocks have comments (%d%%)\n",
		report.Passed, report.Total, percent(report.Passed, report.Total)))

	failed := report.Total - report.Passed
	if failed > 0 {
		sb.WriteString(fmt.Sprintf("  %d blocks need comments — run with --fix to add suggestions\n", failed))
	}

	return sb.String()
}

// exprToString 將 AST expr 轉為字串（用於 receiver type）
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	default:
		return "?"
	}
}

func percent(a, b int) int {
	if b == 0 {
		return 0
	}
	return a * 100 / b
}
