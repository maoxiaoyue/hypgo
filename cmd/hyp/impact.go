package main

import (
	"fmt"

	"github.com/maoxiaoyue/hypgo/pkg/impact"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact [file.go]",
	Short: "Analyze change impact of a Go source file",
	Long: `Analyze what packages, routes, and tests would be affected by modifying
a specific Go source file. Run this BEFORE making changes to shared
packages to understand the blast radius.

Output includes:
  - Package the file belongs to
  - Direct dependents (packages that import this package)
  - Affected tests (test files in dependent packages, with test count)
  - Risk level assessment

Risk levels:
  LOW      < 2 dependent packages, < 20 affected tests
  MEDIUM   2-4 dependent packages or 20-49 affected tests
  HIGH     >= 5 dependent packages or >= 50 affected tests

Example output:
  Impact Analysis: pkg/errors/catalog.go
  Package: pkg/errors

  Direct dependents (import this package):
    → pkg/contract
    → pkg/diagnostic
    → pkg/scaffold

  Affected tests:
    → pkg/errors/*_test.go (19 tests)
    → pkg/contract/*_test.go (24 tests)
    Total: 43 tests

  Risk: MEDIUM (3 packages depend on this)

Safety:
  - Read-only analysis — never modifies any files
  - Only scans import paths, never reads function bodies
  - Path validated to be within project directory

Examples:
  hyp impact pkg/errors/catalog.go
  hyp impact pkg/router/router.go
  hyp impact app/controllers/user.go`,
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
