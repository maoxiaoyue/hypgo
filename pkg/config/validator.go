package config

import (
	"fmt"
	"os"
)

func ValidateConfig(cfg *Config) error {
	// 檢查必要的配置
	if cfg.Server.Addr == "" {
		return fmt.Errorf("server address is required")
	}

	// 生產環境檢查
	if os.Getenv("HYPGO_ENV") == "production" {
		if !cfg.Server.TLS.Enabled {
			return fmt.Errorf("TLS must be enabled in production")
		}

		if cfg.Logger.Level == "debug" {
			return fmt.Errorf("debug logging should not be used in production")
		}
	}

	return nil
}
