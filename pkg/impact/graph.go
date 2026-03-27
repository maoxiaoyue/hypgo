package impact

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// BuildImportGraph 掃描專案目錄，建立套件 import 依賴圖
// 回傳 map[套件路徑][]被 import 的套件路徑
//
// 僅掃描 .go 檔案（排除 _test.go），用於分析正式程式碼的依賴關係
func BuildImportGraph(root string) (map[string][]string, error) {
	graph := make(map[string][]string)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳過無法存取的檔案
		}

		// 跳過隱藏目錄和 vendor
		name := info.Name()
		if info.IsDir() {
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// 只處理 .go 檔案（含 _test.go，因為測試也是影響範圍）
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		// 解析 import
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // 跳過無法解析的檔案
		}

		// 取得此檔案所屬的套件路徑
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil
		}
		pkgPath := filepath.ToSlash(relDir)

		// 收集 import 路徑
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			graph[pkgPath] = append(graph[pkgPath], importPath)
		}

		return nil
	})

	return graph, err
}

// FindDependents 找出直接 import 指定套件的所有套件路徑
//
// targetPkg 可以是相對路徑（如 "pkg/errors"）或完整 module 路徑
func FindDependents(graph map[string][]string, targetPkg string) []string {
	var dependents []string

	// 標準化 target
	target := filepath.ToSlash(targetPkg)

	for pkg, imports := range graph {
		if pkg == target {
			continue // 排除自己
		}
		for _, imp := range imports {
			// 比對完整路徑或相對路徑後綴
			if imp == target || strings.HasSuffix(imp, "/"+target) || strings.HasSuffix(imp, "/"+filepath.Base(target)) {
				dependents = append(dependents, pkg)
				break
			}
		}
	}

	return dependents
}

// countTestsInPackage 計算指定套件目錄中的測試函式數量
func countTestsInPackage(root, pkgPath string) int {
	dir := filepath.Join(root, pkgPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	count := 0
	fset := token.NewFileSet()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") {
				count++
			}
		}
	}

	return count
}
