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

SHALLOW (default) — runs the project to collect its current routes (the same
way as hyp context: the main package is built and run with HYPGO_MANIFEST_OUT;
server.Start() writes the manifest to a temporary file and returns without
listening) and reports declaration-level gaps:
  no_schema            Route registered without r.Schema(...)
  missing_input        POST/PUT/PATCH with a Schema but no Input declared
                       (set NoInput: true when there is no request body)
  missing_output       Non-DELETE with a Schema but no Output declared
                       (declare a 204 in Responses when nothing is returned)
  missing_summary      Schema present but Summary empty (info level)
  unregistered_schema  Schema registered but the router has no such route (info level)

Running the project means main() runs up to server.Start(): it has to get there
without a database or other services, and whatever it does on the way (opening
its log file, for instance) happens. A program that is still running after
--timeout (default 1m) is stopped and the check fails; that is what a gRPC
project does, since it never calls server.Start(). Use --no-refresh for those.
CLI / Desktop projects run ./tools/genctx instead; a genctx generated before
v0.9.3 rewrites .hyp/context.yaml.

With --no-refresh (or -f) the check reads an existing manifest file instead of
running the project. That file is only as fresh as the last server start or
hyp context run: routes added since then are not seen.

DEEP (--deep) — statically analyzes Go source (go/types), no running server or
traffic required. For every route registered with r.Schema(...).Handle(h) or
RegisterSchema it checks the handler (and helpers it passes c to) binds the
declared Input via c.BindInput:
  deep:input_mismatch  handler binds a type different from the declared Input
  deep:no_bindinput    writable handler never calls c.BindInput at all
  deep:input_unbound   GET / DELETE handler with a declared Input never calls
                       c.BindInput (info level: only fails with --strict)
Every handler passed to Handle(...) is scanned, so the binding may live in an
earlier one. Positions are relative to the module root.
This promotes the "bound wrong struct" silent failure — previously only the
runtime BindInput reporter could catch it (and only when a request fired) — into
a build-time CI gate. Routes that cannot be resolved statically are listed as
skipped (never silently counted as passing).

Exit codes:
  0   no actionable warnings
  1   one or more warnings (info-level ones only fail with --strict); the project
      could not be run to collect its routes;
      any deep finding or package load error (compile errors, patterns matching
      nothing); skipped routes with --strict; a manifest with no lint section;
      a manifest named with -f that cannot be read; package arguments without --deep

Paths: the manifest and the default ./... scan are relative to the module root
(nearest go.mod going up), so the result doesn't depend on the current directory.
A -f path and package patterns are relative to the current directory.

Flags:
  -f, --file     Read this manifest file instead of running the project
  --no-refresh   Read .hyp/context.yaml instead of running the project
  -p, --pkg      Main package to run (default: .)
  --timeout      How long the project may run before it is stopped (default: 1m)
  --deep         Also run the static source-level Schema↔handler check
  --strict       Also fail on info-level findings and skipped routes
  --quiet        Print only the shallow coverage summary line

Examples:
  hyp lint                      Run the project and check its routes
  hyp lint --no-refresh         Check the existing .hyp/context.yaml
  hyp lint --deep               Shallow + deep (deep scans the whole module)
  hyp lint --deep ./pkg/api/... Deep check restricted to a package pattern
  hyp lint --deep --strict      Also fail when any route could not be verified`,
	RunE:          runLint,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	lintCmd.Flags().StringP("file", "f", ".hyp/context.yaml", "Read this manifest file instead of running the project")
	lintCmd.Flags().Bool("no-refresh", false, "Read .hyp/context.yaml instead of running the project")
	lintCmd.Flags().StringP("pkg", "p", ".", "Main package to run")
	lintCmd.Flags().Duration("timeout", defaultCollectTimeout, "How long the project may run before it is stopped (compilation not included)")
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
	noRefresh, _ := cmd.Flags().GetBool("no-refresh")
	pkg, _ := cmd.Flags().GetString("pkg")

	// ── 淺層（manifest）檢查 ──
	// 預設實際執行專案取得目前的路由。先前只讀磁碟上的 .hyp/context.yaml：它只在 server 啟動或
	// hyp context 時更新，新增一條沒有 Schema 的路由後沒重跑，CI 用的是舊檔而照樣通過
	if !explicitFile && !noRefresh {
		m, cerr := collectManifest(cmd, root, pkg)
		if cerr != nil {
			return fmt.Errorf("cannot collect the project's routes: %w\n  hint: -p <pkg> selects the main package; --no-refresh checks the existing %s instead", cerr, file)
		}
		n, perr := shallowReport(m, "the manifest collected from the project", strict, quiet)
		if perr != nil {
			return perr
		}
		hardFailures += n
	} else if data, err := os.ReadFile(file); err != nil {
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
			hardFailures += len(rep.Skipped) + len(rep.Info)
		}
	}

	if hardFailures > 0 {
		return fmt.Errorf("schema lint failed: %d issue(s)", hardFailures)
	}
	return nil
}

// runShallow 解析 manifest 檔並印出淺層報告，回傳 hard failure 數。
// @ai:generated by=claude-opus-4-8 date=2026-06-15
func runShallow(data []byte, file string, strict, quiet bool) (int, error) {
	// yaml.Unmarshal 同時可解析 JSON（YAML 為 JSON 超集）
	var m manifest.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("invalid manifest %q: %w", file, err)
	}
	return shallowReport(&m, fmt.Sprintf("manifest %q", file), strict, quiet)
}

// shallowReport 印出 manifest 的 lint 區段，回傳 hard failure 數（資訊級的警告僅 --strict 計入）
func shallowReport(m *manifest.Manifest, source string, strict, quiet bool) (int, error) {
	// 先前印「Nothing to check」並 exit 0：manifest 過舊或不完整時 CI gate 靜默通過
	if m.Lint == nil {
		return 0, fmt.Errorf("%s has no lint section (generated by an older hypgo, or not by AutoSync); regenerate it with `hyp context -o .hyp/context.yaml` or by starting the server once", source)
	}

	if quiet {
		fmt.Printf("Schema coverage: %s (%d warning(s))\n", m.Lint.Coverage, len(m.Lint.Warnings))
	} else {
		fmt.Println(manifest.FormatLint(m.Lint))
	}

	hard := 0
	for _, w := range m.Lint.Warnings {
		if !manifest.InfoKind(w.Kind) {
			hard++
		}
	}
	if strict {
		return len(m.Lint.Warnings), nil
	}
	return hard, nil
}
