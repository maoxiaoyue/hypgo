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
	Long: `Scan a Go source file and check all exported blocks (package, type, func,
method, const, var) for standardized documentation comments.

Missing comments will be reported with suggested text. Use --fix to
automatically add the suggested comments to the file.

Examples:
  hyp chkcomment controllers/user.go
  hyp chkcomment --fix models/order.go`,
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
