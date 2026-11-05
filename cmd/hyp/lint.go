package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maoxiaoyue/hypgo/pkg/lintdeep"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var lintCmd = &cobra.Command{
	Use:   "lint [packages]",
	Short: "Check Schema completeness (manifest) and Schema↔handler alignment (--deep)",
	Long: `Check Schema completeness using the lint section of the project manifest,
and optionally run a static (compile-time) Schema↔handler alignment check.

SHALLOW (default) — reads the manifest produced by the server's AutoSync on
Server.Start() (.hyp/context.yaml) and reports declaration-level gaps:
  no_schema        Route registered without r.Schema(...)
  missing_input    POST/PUT/PATCH with a Schema but no Input declared
  missing_output   Non-DELETE with a Schema but no Output declared
  missing_summary  Schema present but Summary empty (info level)

DEEP (--deep) — statically analyzes Go source (go/types), no running server or
traffic required. For every route registered with r.Schema(...).Handle(h) or
RegisterSchema it checks the handler (and helpers it passes c to) binds the
declared Input via c.BindInput:
  deep:input_mismatch  handler binds a type different from the declared Input
  deep:no_bindinput    writable handler never calls c.BindInput at all
This promotes the "bound wrong struct" silent failure — previously only the
runtime BindInput reporter could catch it (and only when a request fired) — into
a build-time CI gate. Routes that cannot be resolved statically are listed as
skipped (never silently counted as passing).

Exit codes:
  0   no actionable warnings
  1   one or more warnings (info-level missing_summary only fails with --strict);
      any deep finding or package load error (compile errors, patterns matching
      nothing); skipped routes with --strict; a manifest with no lint section;
      a manifest named with -f that cannot be read; package arguments without --deep

Paths: the manifest and the default ./... scan are relative to the module root
(nearest go.mod going up), so the result doesn't depend on the current directory.
A -f path and package patterns are relative to the current directory.

Flags:
  -f, --file     Manifest file for the shallow check (default: .hyp/context.yaml)
  --deep         Also run the static source-level Schema↔handler check
  --strict       Also fail on info-level findings (missing_summary) and skipped routes
  --quiet        Print only the shallow coverage summary line

Examples:
  hyp lint                      Shallow check of .hyp/context.yaml
  hyp lint --deep               Shallow + deep (deep scans the whole module)
  hyp lint --deep ./pkg/api/... Deep check restricted to a package pattern
  hyp lint --deep --strict      Also fail when any route could not be verified`,
	RunE:          runLint,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	lintCmd.Flags().StringP("file", "f", ".hyp/context.yaml", "Manifest file to lint")
	lintCmd.Flags().Bool("deep", false, "Run static (compile-time) Schema↔handler alignment check")
	lintCmd.Flags().Bool("strict", false, "Also fail on info-level findings and routes --deep could not verify")
	lintCmd.Flags().Bool("quiet", false, "Print only the coverage summary line")
}

func runLint(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	deep, _ := cmd.Flags().GetBool("deep")
	strict, _ := cmd.Flags().GetBool("strict")
	quiet, _ := cmd.Flags().GetBool("quiet")

	// 先前未帶 --deep 時套件參數被靜默忽略，使用者以為只檢查了指定套件
	if len(args) > 0 && !deep {
		return fmt.Errorf("package arguments (%v) are only used by --deep; the shallow check always reads the whole manifest", args)
	}

	// 預設路徑以專案根目錄為準：先前在子目錄執行 --deep 只分析該子目錄並 exit 0
	root := moduleRoot(".")
	explicitFile := cmd.Flags().Changed("file")
	if !explicitFile {
		file = filepath.Join(root, file)
	}

	hardFailures := 0

	// ── 淺層（manifest）檢查 ──
	data, err := os.ReadFile(file)
	if err != nil {
		// 明確指定的 -f 讀不到一定是打錯：先前帶 --deep 時只印一行就略過
		if !deep || explicitFile {
			return fmt.Errorf("cannot read manifest %q: %w\n  hint: start the server once so AutoSync writes %s, or run `hyp context -o %s`", file, err, file, file)
		}
		fmt.Printf("（略過 manifest 淺層檢查：無法讀取 %s；改以 --deep 直接分析原始碼）\n", file)
	} else {
		n, perr := runShallow(data, file, strict, quiet)
		if perr != nil {
			return perr
		}
		hardFailures += n
	}

	// ── 深層（原始碼）檢查 ──
	if deep {
		dir := root
		if len(args) > 0 {
			dir = "." // 使用者給的套件樣式相對於當前目錄
		}
		rep, derr := lintdeep.Analyze(dir, args...)
		if derr != nil {
			return fmt.Errorf("deep analyze failed: %w", derr)
		}
		fmt.Println()
		fmt.Println(lintdeep.Format(rep))
		// 載入錯誤代表分析不完整：先前只有 --strict 才算失敗，打錯樣式或編譯不過都會通過
		hardFailures += len(rep.Findings) + len(rep.LoadErrors)
		if strict {
			hardFailures += len(rep.Skipped)
		}
	}

	if hardFailures > 0 {
		return fmt.Errorf("schema lint failed: %d issue(s)", hardFailures)
	}
	return nil
}

// runShallow 解析 manifest、印出淺層報告，回傳 hard failure 數（missing_summary 僅 --strict 計入）。
// @ai:generated by=claude-opus-4-8 date=2026-06-15
func runShallow(data []byte, file string, strict, quiet bool) (int, error) {
	// yaml.Unmarshal 同時可解析 JSON（YAML 為 JSON 超集）
	var m manifest.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("invalid manifest %q: %w", file, err)
	}

	// 先前印「Nothing to check」並 exit 0：manifest 過舊或不完整時 CI gate 靜默通過
	if m.Lint == nil {
		return 0, fmt.Errorf("manifest %q has no lint section (generated by an older hypgo, or not by AutoSync); regenerate it with `hyp context -o %s` or by starting the server once", file, file)
	}

	if quiet {
		fmt.Printf("Schema coverage: %s (%d warning(s))\n", m.Lint.Coverage, len(m.Lint.Warnings))
	} else {
		fmt.Println(manifest.FormatLint(m.Lint))
	}

	hard := 0
	for _, w := range m.Lint.Warnings {
		if w.Kind != "missing_summary" {
			hard++
		}
	}
	if strict {
		return len(m.Lint.Warnings), nil
	}
	return hard, nil
}
