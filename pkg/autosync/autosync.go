// Package autosync 提供 .hyp/context.yaml 自動同步功能
// 在 Server 啟動時自動生成專案 manifest，永遠與程式碼同步
package autosync

import (
	"os"
	"path/filepath"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

// DefaultPath 預設的 context 檔案路徑
const DefaultPath = ".hyp/context.yaml"

// Config 配置 AutoSync 行為
type Config struct {
	// Enabled 是否啟用自動同步（預設 true）
	Enabled bool

	// Path 輸出路徑（預設 .hyp/context.yaml）
	Path string

	// Format 輸出格式（"yaml" 或 "json"，預設 "yaml"）
	Format string
}

// AutoSync 管理 .hyp/context.yaml 的自動同步
type AutoSync struct {
	config Config
	router *router.Router
	appCfg *config.Config
	logger *logger.Logger
}

// New 建立新的 AutoSync 實例
func New(cfg Config, r *router.Router, appCfg *config.Config, log *logger.Logger) *AutoSync {
	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}
	if cfg.Format == "" {
		cfg.Format = "yaml"
	}

	return &AutoSync{
		config: cfg,
		router: r,
		appCfg: appCfg,
		logger: log,
	}
}

// Sync 立即同步 manifest 到檔案
// 安全考量：不包含敏感資訊（密碼、token、DSN）
// 使用原子寫入（temp file + rename）防止中途損壞
func (a *AutoSync) Sync() error {
	if !a.config.Enabled {
		return nil
	}

	// 確保目錄存在
	dir := filepath.Dir(a.config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 收集 manifest（使用已有的 Collector）
	collector := manifest.NewCollector(a.router, a.appCfg)
	m := collector.Collect()

	// 原子寫入：先寫 temp file，再 rename
	// 防止寫入中途斷電或磁碟滿導致 context.yaml 損壞
	tmpPath := a.config.Path + ".tmp"
	if err := manifest.SaveToFile(tmpPath, m, a.config.Format); err != nil {
		os.Remove(tmpPath) // 清理失敗的 temp file
		return err
	}
	if err := os.Rename(tmpPath, a.config.Path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if a.logger != nil {
		a.logger.Info("AutoSync: manifest saved to %s", a.config.Path)
	}

	return nil
}

// SyncSafe 同步但不 panic（用於非關鍵路徑）
func (a *AutoSync) SyncSafe() {
	if err := a.Sync(); err != nil {
		if a.logger != nil {
			a.logger.Warning("AutoSync failed: %v", err)
		}
	}
}
