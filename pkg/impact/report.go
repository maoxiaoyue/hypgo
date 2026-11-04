package impact

import (
	"encoding/json"
	"fmt"
	"strings"
)

// reportListLimit 文字報告每個清單最多列幾項（報告常被貼給 AI，完整內容在 JSON）
const reportListLimit = 15

// FormatReport 格式化影響報告為人類可讀字串
func FormatReport(report *ImpactReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Impact Analysis: %s\n", report.Target)
	fmt.Fprintf(&sb, "Package: %s\n\n", report.Package)

	fmt.Fprintf(&sb, "Risk: %s\n", report.Risk)
	for _, reason := range report.RiskReasons {
		fmt.Fprintf(&sb, "  - %s\n", reason)
	}
	sb.WriteString("\n")

	for _, w := range report.Warnings {
		fmt.Fprintf(&sb, "Warning: %s\n", w)
	}
	if len(report.Warnings) > 0 {
		sb.WriteString("\n")
	}

	if !report.IsTestFile {
		writeAPI(&sb, report.API)
		writeDependents(&sb, report)
		writeRoutes(&sb, report)
	}
	writeTests(&sb, report)

	if len(report.NextSteps) > 0 {
		sb.WriteString("Next steps:\n")
		for _, step := range report.NextSteps {
			fmt.Fprintf(&sb, "  %s\n", step)
		}
	}
	return sb.String()
}

// FormatJSON 輸出完整報告（JSON），供 AI 工具與 CI 使用
func FormatJSON(report *ImpactReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func writeAPI(sb *strings.Builder, api []SymbolUsage) {
	if len(api) == 0 {
		return
	}
	var used, unused, untracked []string
	hasMethods := false
	for _, s := range api {
		hasMethods = hasMethods || len(s.Methods) > 0 || !s.Tracked
		switch {
		case !s.Tracked:
			untracked = append(untracked, s.Name)
		case s.Uses == 0:
			unused = append(unused, s.Name)
		default:
			line := fmt.Sprintf("  %s (%s): %d use(s) in %s", s.Name, s.Kind, s.Uses, limitList(s.Packages, 4))
			if len(s.Methods) > 0 {
				line += "; methods: " + limitList(s.Methods, 6)
			}
			used = append(used, line)
		}
	}
	sb.WriteString("Exported API in this file:\n")
	for i, line := range used {
		if i == reportListLimit {
			fmt.Fprintf(sb, "  ... and %d more\n", len(used)-i)
			break
		}
		sb.WriteString(line + "\n")
	}
	if len(unused) > 0 {
		fmt.Fprintf(sb, "  Not referenced by other packages in this module: %s\n", limitList(unused, reportListLimit))
	}
	if len(untracked) > 0 {
		fmt.Fprintf(sb, "  Methods on types declared elsewhere (callers not tracked): %s\n", limitList(untracked, reportListLimit))
	}
	if hasMethods {
		sb.WriteString("  Method calls are not tracked: other packages may also use this file through them.\n")
	}
	sb.WriteString("\n")
}

func writeDependents(sb *strings.Builder, report *ImpactReport) {
	if len(report.Dependents) == 0 {
		sb.WriteString("Direct dependents: none\n\n")
	} else {
		fmt.Fprintf(sb, "Direct dependents (%d):\n", len(report.Dependents))
		for i, dep := range report.Dependents {
			if i == reportListLimit {
				fmt.Fprintf(sb, "  ... and %d more\n", len(report.Dependents)-i)
				break
			}
			fmt.Fprintf(sb, "  → %s", dep.Path)
			if dep.TestOnly {
				sb.WriteString(" (tests only)")
			}
			if len(dep.Uses) > 0 {
				fmt.Fprintf(sb, "  uses: %s", limitList(dep.Uses, 6))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if len(report.Transitive) > 0 {
		fmt.Fprintf(sb, "Transitive dependents (%d):\n", len(report.Transitive))
		for i, dep := range report.Transitive {
			if i == reportListLimit {
				fmt.Fprintf(sb, "  ... and %d more\n", len(report.Transitive)-i)
				break
			}
			fmt.Fprintf(sb, "  → %s (via %s)\n", dep.Path, dep.Via)
		}
		sb.WriteString("\n")
	}
}

func writeRoutes(sb *strings.Builder, report *ImpactReport) {
	if !report.RoutesKnown {
		sb.WriteString("Routes: unknown (no .hyp/context.yaml; run hyp context)\n\n")
		return
	}
	if len(report.Routes) == 0 {
		sb.WriteString("Routes: none affected\n\n")
		return
	}
	direct := report.directRouteCount()
	fmt.Fprintf(sb, "Routes (%d direct, %d through affected packages):\n", direct, len(report.Routes)-direct)
	for i, route := range report.Routes {
		if i == reportListLimit {
			fmt.Fprintf(sb, "  ... and %d more\n", len(report.Routes)-i)
			break
		}
		if route.Direct {
			fmt.Fprintf(sb, "  → %s  (%s)\n", route.Route, route.Reason)
		} else {
			fmt.Fprintf(sb, "  → %s\n", route.Route)
		}
	}
	sb.WriteString("\n")
}

func writeTests(sb *strings.Builder, report *ImpactReport) {
	if len(report.Tests) == 0 {
		sb.WriteString("Affected tests: none\n")
	} else {
		fmt.Fprintf(sb, "Affected tests (%d in %d packages):\n", report.totalTests(), len(report.Tests))
		for i, t := range report.Tests {
			if i == reportListLimit {
				fmt.Fprintf(sb, "  ... and %d more packages\n", len(report.Tests)-i)
				break
			}
			fmt.Fprintf(sb, "  → %s (%d tests)\n", t.File, t.TestCount)
		}
	}
	if len(report.UntestedPackages) > 0 {
		fmt.Fprintf(sb, "Affected packages without tests: %s\n", limitList(report.UntestedPackages, reportListLimit))
	}
	sb.WriteString("\n")
}

// limitList 以逗號連接，超過上限的部分以 "+N more" 表示
func limitList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(items[:limit], ", "), len(items)-limit)
}
