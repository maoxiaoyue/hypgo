package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// hypConfig 儲存在 .hyp/config.json 的專案級配置
type hypConfig struct {
	DiffLog bool `json:"diff_log"`
}

const hypConfigPath = ".hyp/config.json"

var diffLogCmd = &cobra.Command{
	Use:   "diff-log",
	Short: "Log AI changes to logs/ai.diff_YYYYMMDD.log",
	Long: `Record current uncommitted changes to a daily log file.

When enabled, AI tools are instructed to run this after making changes,
creating an audit trail of all AI modifications.

Usage:
  hyp diff-log            Record current changes
  hyp diff-log --on       Enable diff logging (AI tools will auto-log)
  hyp diff-log --off      Disable diff logging (saves tokens)
  hyp diff-log --status   Show current setting

The setting is stored in .hyp/config.json (project-level).
When enabled, "hyp ai-rules" includes the logging instruction in
AGENTS.md and other AI tool config files.
When disabled, the instruction is omitted — saving tokens.`,
	RunE: runDiffLog,
}

func init() {
	rootCmd.AddCommand(diffLogCmd)
	diffLogCmd.Flags().Bool("on", false, "Enable diff logging")
	diffLogCmd.Flags().Bool("off", false, "Disable diff logging")
	diffLogCmd.Flags().Bool("status", false, "Show current setting")
}

func runDiffLog(cmd *cobra.Command, args []string) error {
	on, _ := cmd.Flags().GetBool("on")
	off, _ := cmd.Flags().GetBool("off")
	showStatus, _ := cmd.Flags().GetBool("status")

	// 開關控制
	if on {
		return setDiffLog(true)
	}
	if off {
		return setDiffLog(false)
	}
	if showStatus {
		enabled := isDiffLogEnabled()
		if enabled {
			fmt.Println("diff-log: ON")
			fmt.Println("AI tools will log changes to logs/ai.diff_YYYYMMDD.log")
		} else {
			fmt.Println("diff-log: OFF")
			fmt.Println("Enable with: hyp diff-log --on")
		}
		return nil
	}

	// 預設動作：記錄當前改動
	return recordDiffLog()
}

func setDiffLog(enabled bool) error {
	cfg := loadHypConfig()
	cfg.DiffLog = enabled
	if err := saveHypConfig(cfg); err != nil {
		return err
	}

	if enabled {
		fmt.Println("✅ diff-log: ON")
		fmt.Println("   AI tools will log changes after modifications.")
		fmt.Println("   Run 'hyp ai-rules' to update AI config files.")
	} else {
		fmt.Println("❌ diff-log: OFF")
		fmt.Println("   AI config files will not include logging instructions (saves tokens).")
		fmt.Println("   Run 'hyp ai-rules' to update AI config files.")
	}
	return nil
}

func recordDiffLog() error {
	logDir := "logs"
	os.MkdirAll(logDir, 0755)

	date := time.Now().Format("20060102")
	logFile := filepath.Join(logDir, "ai.diff_"+date+".log")
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// 取得 git diff
	diffStat := gitOutput("diff", "--stat", "HEAD")
	diffNum := gitOutput("diff", "--numstat", "HEAD")
	branch := gitOutput("branch", "--show-current")
	untracked := gitOutput("ls-files", "--others", "--exclude-standard")

	if diffStat == "" && untracked == "" {
		fmt.Println("No changes to log.")
		return nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("===== %s =====\n\n", timestamp))
	sb.WriteString(fmt.Sprintf("Branch: %s\n\n", strings.TrimSpace(branch)))

	if diffStat != "" {
		sb.WriteString("--- Changed Files ---\n")
		sb.WriteString(diffStat)
		sb.WriteString("\n\n--- Line Changes (added/deleted/file) ---\n")
		sb.WriteString(diffNum)
		sb.WriteString("\n")
	}

	if untracked != "" {
		sb.WriteString("\n--- New Files (untracked) ---\n")
		for _, f := range strings.Split(strings.TrimSpace(untracked), "\n") {
			if f == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("  + %s\n", f))
		}
	}

	sb.WriteString("\n==========================================\n\n")

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	f.WriteString(sb.String())
	fmt.Printf("Diff logged to %s\n", logFile)
	return nil
}

func gitOutput(args ...string) string {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// IsDiffLogEnabled 供 airules 模板使用
func IsDiffLogEnabled() bool {
	return isDiffLogEnabled()
}

func isDiffLogEnabled() bool {
	cfg := loadHypConfig()
	return cfg.DiffLog
}

func loadHypConfig() hypConfig {
	data, err := os.ReadFile(hypConfigPath)
	if err != nil {
		return hypConfig{}
	}
	var cfg hypConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

func saveHypConfig(cfg hypConfig) error {
	os.MkdirAll(filepath.Dir(hypConfigPath), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hypConfigPath, data, 0644)
}
