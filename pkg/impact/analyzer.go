// Package impact 提供 Change Impact Analysis 功能
// 分析修改某個 Go 檔案後，會影響哪些套件、路由和測試
// 讓 AI 在修改程式碼前先確認影響範圍
package impact

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ImpactReport 分析報告
type ImpactReport struct {
	Target     string          // 分析目標檔案
	Package    string          // 目標所屬套件
	Dependents []DependentFile // 直接依賴此套件的檔案
	Tests      []TestImpact    // 受影響的測試
	Risk       string          // LOW / MEDIUM / HIGH
}

// DependentFile 依賴此套件的檔案
type DependentFile struct {
	Path    string // 檔案路徑
	Package string // 所屬套件名
}

// TestImpact 受影響的測試
type TestImpact struct {
	File      string // 測試檔案路徑
	TestCount int    // 測試數量
}

// Analyze 分析指定檔案的變更影響
//
// 使用範例：
//
//	report, err := impact.Analyze("pkg/errors/catalog.go", ".")
//	fmt.Print(impact.FormatReport(report))
func Analyze(targetFile string, projectRoot string) (*ImpactReport, error) {
	// 安全檢查：限制在專案目錄內
	absTarget, err := filepath.Abs(targetFile)
	if err != nil {
		return nil, fmt.Errorf("invalid target path: %w", err)
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid project root: %w", err)
	}
	if !strings.HasPrefix(absTarget, absRoot) {
		return nil, fmt.Errorf("target file must be within project root")
	}

	// 取得目標套件名（相對於 project root）
	relTarget, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return nil, fmt.Errorf("cannot compute relative path: %w", err)
	}
	targetPkg := packageFromFile(relTarget)

	// 建立 import 依賴圖
	graph, err := BuildImportGraph(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to build import graph: %w", err)
	}

	// 找出直接依賴者
	dependents := FindDependents(graph, targetPkg)

	// 找出受影響的測試
	tests := findAffectedTests(projectRoot, targetPkg, dependents)

	// 計算風險等級
	risk := calculateRisk(dependents, tests)

	// 組裝依賴檔案列表
	depFiles := make([]DependentFile, 0, len(dependents))
	for _, dep := range dependents {
		depFiles = append(depFiles, DependentFile{
			Path:    dep,
			Package: filepath.Base(dep),
		})
	}

	return &ImpactReport{
		Target:     targetFile,
		Package:    targetPkg,
		Dependents: depFiles,
		Tests:      tests,
		Risk:       risk,
	}, nil
}

// FormatReport 格式化影響報告為人類可讀字串
func FormatReport(report *ImpactReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Impact Analysis: %s\n", report.Target))
	sb.WriteString(fmt.Sprintf("Package: %s\n\n", report.Package))

	// 直接依賴者
	if len(report.Dependents) > 0 {
		sb.WriteString("Direct dependents (import this package):\n")
		for _, dep := range report.Dependents {
			sb.WriteString(fmt.Sprintf("  → %s\n", dep.Path))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Direct dependents: none\n\n")
	}

	// 受影響的測試
	if len(report.Tests) > 0 {
		totalTests := 0
		sb.WriteString("Affected tests:\n")
		for _, t := range report.Tests {
			sb.WriteString(fmt.Sprintf("  → %s (%d tests)\n", t.File, t.TestCount))
			totalTests += t.TestCount
		}
		sb.WriteString(fmt.Sprintf("  Total: %d tests\n\n", totalTests))
	} else {
		sb.WriteString("Affected tests: none\n\n")
	}

	sb.WriteString(fmt.Sprintf("Risk: %s (%d packages depend on this)\n",
		report.Risk, len(report.Dependents)))

	return sb.String()
}

// packageFromFile 從檔案路徑推導套件路徑（相對於 project root）
func packageFromFile(file string) string {
	dir := filepath.Dir(file)
	// 標準化為 forward slash
	dir = filepath.ToSlash(dir)
	return dir
}

// findAffectedTests 找出受影響的測試檔案
func findAffectedTests(root string, targetPkg string, dependents []string) []TestImpact {
	var tests []TestImpact

	// 目標套件自身的測試
	if count := countTestsInPackage(root, targetPkg); count > 0 {
		tests = append(tests, TestImpact{
			File:      filepath.Join(targetPkg, "*_test.go"),
			TestCount: count,
		})
	}

	// 依賴套件的測試
	for _, dep := range dependents {
		if count := countTestsInPackage(root, dep); count > 0 {
			tests = append(tests, TestImpact{
				File:      filepath.Join(dep, "*_test.go"),
				TestCount: count,
			})
		}
	}

	return tests
}

// calculateRisk 根據影響範圍計算風險等級
func calculateRisk(dependents []string, tests []TestImpact) string {
	totalTests := 0
	for _, t := range tests {
		totalTests += t.TestCount
	}

	depCount := len(dependents)

	if depCount >= 5 || totalTests >= 50 {
		return "HIGH"
	}
	if depCount >= 2 || totalTests >= 20 {
		return "MEDIUM"
	}
	return "LOW"
}
