package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maoxiaoyue/hypgo/pkg/airules"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var aiRulesCmd = &cobra.Command{
	Use:   "ai-rules",
	Short: "Generate AI tool configuration files",
	Long: `Generate configuration files for AI coding tools so they understand
HypGo conventions and can collaborate more efficiently.

Supported tools:
  agents    AGENTS.md                         Codex, Cursor, Aider, OpenHands
  gemini    GEMINI.md                         Google Gemini CLI / AI Studio
  copilot   .github/copilot-instructions.md   GitHub Copilot
  cursor    .cursor/rules/hypgo.mdc           Cursor (scoped rules)
  windsurf  .windsurf/rules/hypgo.md          Windsurf

Files with the auto-generated marker are overwritten on re-run.
Manually created files (without the marker) are never overwritten.

Examples:
  hyp ai-rules
  hyp ai-rules --only agents,gemini
  hyp ai-rules --dry-run`,
	RunE: runAIRules,
}

func init() {
	rootCmd.AddCommand(aiRulesCmd)
	aiRulesCmd.Flags().String("only", "", "Comma-separated list of targets (e.g., agents,gemini,copilot)")
	aiRulesCmd.Flags().String("dir", ".", "Project root directory")
	aiRulesCmd.Flags().Bool("dry-run", false, "Preview output without writing files")
}

func runAIRules(cmd *cobra.Command, args []string) error {
	only, _ := cmd.Flags().GetString("only")
	dir, _ := cmd.Flags().GetString("dir")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	m := loadManifestIfExists(dir)

	targets := airules.FilterTargets(airules.AllTargets(), only)
	if len(targets) == 0 {
		return fmt.Errorf("no matching targets found for: %s", only)
	}

	opts := airules.Options{
		DiffLogEnabled: IsDiffLogEnabled(),
	}
	results, err := airules.GenerateAll(dir, targets, m, opts, dryRun)
	if err != nil {
		return err
	}

	for _, r := range results {
		rel, _ := filepath.Rel(dir, r.Path)
		if rel == "" {
			rel = r.Path
		}
		switch r.Status {
		case airules.StatusCreated:
			fmt.Printf("  + %s\n", rel)
		case airules.StatusSkipped:
			fmt.Printf("  ~ %s (skipped: %s)\n", rel, r.Message)
		case airules.StatusDryRun:
			fmt.Printf("  [dry-run] %s\n", rel)
			fmt.Println(r.Content)
			fmt.Println("---")
		case airules.StatusError:
			fmt.Fprintf(os.Stderr, "  ! %s: %s\n", rel, r.Message)
		}
	}

	if !dryRun {
		created := 0
		for _, r := range results {
			if r.Status == airules.StatusCreated {
				created++
			}
		}
		fmt.Printf("\n%d file(s) generated.\n", created)
	}

	return nil
}

func loadManifestIfExists(dir string) *manifest.Manifest {
	path := filepath.Join(dir, ".hyp", "context.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m manifest.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}
