package main

import (
	"fmt"
	"os"

	"github.com/maoxiaoyue/hypgo/pkg/annotation"
	"github.com/spf13/cobra"
)

var chkcommentCmd = &cobra.Command{
	Use:   "chkcomment [file.go]",
	Short: "Check and add standardized comments to Go source files",
	Long: `Scan a Go source file for exported blocks (package, type, func, method,
const, var) and check whether each has a standardized documentation comment.

This command helps maintain consistent code documentation, which is
essential for AI collaboration — AI tools use comments to understand
business constraints and code intent without reading implementation details.

Output includes:
  - Coverage summary (e.g., "3/5 blocks have comments (60%)")
  - Per-block results showing which blocks are missing comments
  - Suggested comment text for each missing block

The --fix flag automatically adds the suggested comments to the file.
Before modifying, it creates a .bak backup of the original file.

Annotation Protocol support:
  This checker recognizes @ai: annotations in comments:
    // @ai:constraint max_items=100
    // @ai:deprecated use V2 instead
    // @ai:security requires_auth
    // @ai:impact routes=/api/users
    // @ai:owner team=backend

Flags:
  --fix   Automatically add suggested comments (creates .bak backup)

Safety:
  - Pure AST analysis — never executes your code
  - Only accepts .go files, rejects symlinks
  - --fix always creates a backup before modifying

Examples:
  hyp chkcomment controllers/user.go             Check and report
  hyp chkcomment --fix models/order.go            Auto-add comments
  hyp chkcomment pkg/errors/catalog.go            Check a pkg file`,
	Args: cobra.ExactArgs(1),
	RunE: runChkComment,
}

func init() {
	chkcommentCmd.Flags().Bool("fix", false, "Automatically add suggested comments to the file")
}

func runChkComment(cmd *cobra.Command, args []string) error {
	filename := args[0]
	fix, _ := cmd.Flags().GetBool("fix")

	// 檢查檔案
	report, err := annotation.CheckFile(filename)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	// 輸出報告
	fmt.Print(annotation.FormatReport(report))

	// 自動修復模式
	if fix && report.Passed < report.Total {
		if err := annotation.FixFile(filename, report.Results); err != nil {
			return fmt.Errorf("fix failed: %w", err)
		}
		fmt.Fprintf(os.Stdout, "\nFixed: comments added to %s (backup: %s.bak)\n", filename, filename)
	}

	return nil
}
