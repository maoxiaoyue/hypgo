package main

import (
	"fmt"

	"github.com/maoxiaoyue/hypgo/pkg/impact"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact [file.go]",
	Short: "Analyze change impact of a Go source file",
	Long: `Analyze what routes, tests, and downstream packages would be affected
by modifying a specific Go source file.

Run this before making changes to understand the blast radius.

Examples:
  hyp impact pkg/errors/catalog.go
  hyp impact pkg/router/router.go`,
	Args: cobra.ExactArgs(1),
	RunE: runImpact,
}

func runImpact(cmd *cobra.Command, args []string) error {
	targetFile := args[0]

	report, err := impact.Analyze(targetFile, ".")
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	fmt.Print(impact.FormatReport(report))
	return nil
}
