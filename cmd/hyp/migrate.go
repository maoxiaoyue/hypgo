package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maoxiaoyue/hypgo/pkg/migrate"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration tools",
	Long: `Database migration tools for generating and managing schema migrations.

Subcommands:
  diff     Compare current models with snapshot and generate SQL migrations
  snapshot Save current model schema as snapshot for future diff`,
}

var migrateDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Generate migration SQL from model changes",
	Long: `Compare current Go model structs (registered via ModelRegistry) against
the stored snapshot, and generate up/down SQL migration files.

The snapshot is stored at .hyp/schema_snapshot.json by default.

Examples:
  hyp migrate diff
  hyp migrate diff --dialect postgres
  hyp migrate diff --dialect mysql --output migrations/`,
	RunE: runMigrateDiff,
}

var migrateSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Save current schema as snapshot",
	Long: `Scan all registered models and save the current schema to the snapshot file.
This snapshot is used as the baseline for future diff operations.`,
	RunE: runMigrateSnapshot,
}

func init() {
	migrateDiffCmd.Flags().StringP("dialect", "d", "postgres", "SQL dialect: postgres or mysql")
	migrateDiffCmd.Flags().StringP("output", "o", "migrations/", "Output directory for migration files")
	migrateDiffCmd.Flags().StringP("snapshot", "s", ".hyp/schema_snapshot.json", "Snapshot file path")

	migrateSnapshotCmd.Flags().StringP("snapshot", "s", ".hyp/schema_snapshot.json", "Snapshot file path")

	migrateCmd.AddCommand(migrateDiffCmd)
	migrateCmd.AddCommand(migrateSnapshotCmd)
}

func runMigrateDiff(cmd *cobra.Command, args []string) error {
	dialect, _ := cmd.Flags().GetString("dialect")
	outputDir, _ := cmd.Flags().GetString("output")
	snapshotPath, _ := cmd.Flags().GetString("snapshot")

	// 載入快照
	snapshot, err := migrate.LoadSnapshot(snapshotPath)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w", err)
	}

	// 取得全域 registry 的 models
	registry := migrate.GlobalRegistry()
	if registry.Len() == 0 {
		fmt.Println("No models registered. Use migrate.GlobalRegistry().Register() in your app to register models.")
		return nil
	}

	// 掃描 models
	tables := migrate.ScanModels(registry)

	// 計算 diff
	changes := migrate.Diff(tables, snapshot)
	if len(changes) == 0 {
		fmt.Println("No schema changes detected.")
		return nil
	}

	// 顯示變更
	fmt.Printf("Detected %d change(s):\n", len(changes))
	for _, c := range changes {
		fmt.Printf("  • %s\n", c.String())
	}

	// 產生 SQL
	up, down := migrate.GenerateSQL(changes, dialect)

	// 儲存到檔案
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	upFile, downFile := migrate.MigrationFiles(outputDir + "/")

	if err := os.WriteFile(upFile, []byte(up), 0644); err != nil {
		return fmt.Errorf("failed to write up migration: %w", err)
	}
	if err := os.WriteFile(downFile, []byte(down), 0644); err != nil {
		return fmt.Errorf("failed to write down migration: %w", err)
	}

	fmt.Printf("\nMigration files generated:\n  UP:   %s\n  DOWN: %s\n", upFile, downFile)

	// 更新快照
	if err := migrate.SaveSnapshot(snapshotPath, tables); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}
	fmt.Printf("Snapshot updated: %s\n", snapshotPath)

	return nil
}

func runMigrateSnapshot(cmd *cobra.Command, args []string) error {
	snapshotPath, _ := cmd.Flags().GetString("snapshot")

	registry := migrate.GlobalRegistry()
	if registry.Len() == 0 {
		fmt.Println("No models registered.")
		return nil
	}

	tables := migrate.ScanModels(registry)

	// 確保目錄存在
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := migrate.SaveSnapshot(snapshotPath, tables); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	fmt.Printf("Snapshot saved: %s (%d tables)\n", snapshotPath, len(tables))
	return nil
}
