package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/annotation"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/spf13/cobra"
)

var chkcommentCmd = &cobra.Command{
	Use:   "chkcomment [file.go]",
	Short: "Check and add standardized comments to Go source files",
	Long: `Scan a Go source file for exported blocks (package, type, func, method,
const, var) and check whether each has a standardized documentation comment.

Missing comments will be reported with suggested text. Use --fix to
automatically add the suggested comments to the file. When an LLM config
is supplied (or auto-detected at config/llm.yaml / .hyp/llm.yaml), --fix
will additionally insert structured @ai: annotations produced by the LLM
(or by built-in heuristics when mode=none).

Examples:
  hyp chkcomment controllers/user.go
  hyp chkcomment --fix models/order.go
  hyp chkcomment --fix --llm config/llm.yaml controllers/user.go`,
	Args: cobra.ExactArgs(1),
	RunE: runChkComment,
}

func init() {
	chkcommentCmd.Flags().Bool("fix", false, "Automatically add suggested comments to the file")
	chkcommentCmd.Flags().String("llm", "", "Path to LLM config YAML (default: auto-detect config/llm.yaml or .hyp/llm.yaml)")
}

func runChkComment(cmd *cobra.Command, args []string) error {
	filename := args[0]
	fix, _ := cmd.Flags().GetBool("fix")
	llmPath, _ := cmd.Flags().GetString("llm")

	report, err := annotation.CheckFile(filename)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	fmt.Print(annotation.FormatReport(report))

	if !fix {
		return nil
	}

	resolvedPath := resolveLLMConfigPath(llmPath)
	llmCfg, err := config.LoadLLMConfig(resolvedPath)
	if err != nil {
		return fmt.Errorf("load llm config: %w", err)
	}

	suggester, err := annotation.NewSuggester(llmCfg)
	if err != nil {
		return fmt.Errorf("init suggester: %w", err)
	}

	source := "heuristic"
	if llmCfg.Mode != "" && llmCfg.Mode != config.LLMModeNone {
		source = string(llmCfg.Mode)
	}
	if resolvedPath != "" {
		fmt.Fprintf(os.Stdout, "\nUsing LLM config: %s (mode=%s)\n", resolvedPath, source)
	} else {
		fmt.Fprintf(os.Stdout, "\nNo LLM config found — using built-in heuristics\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Git headline enrichment：只在 LLM 模式下執行（heuristic 不需要 git 脈絡）
	if llmCfg.Mode != "" && llmCfg.Mode != config.LLMModeNone {
		enriched, enrichErr := annotation.EnrichMissingHeadlines(ctx, filename, report.Results, suggester)
		if enrichErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: headline enrichment failed: %v\n", enrichErr)
		} else if enriched > 0 {
			fmt.Fprintf(os.Stdout, "Enriched %d placeholder headline(s) using git history.\n", enriched)
		}
	}

	// 補齊缺少的 @ai: 欄位（若全部已通過則跳過）
	if report.Passed >= report.Total {
		return nil
	}

	if err := annotation.FixFileWithSuggester(ctx, filename, report.Results, suggester); err != nil {
		return fmt.Errorf("fix failed: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Fixed: comments added to %s (backup: %s.bak)\n", filename, filename)
	return nil
}

// resolveLLMConfigPath 處理 --llm 參數及預設探測順序
func resolveLLMConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, candidate := range []string{"config/llm.yaml", ".hyp/llm.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
