package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// 這個檔案放 hyp restart 送訊號之前的判斷，不分平台（送訊號本身只有 Unix，見 restart.go），
// 所以在 Windows 上也測得到

// restartConfigPaths 與 hypgo.New() 找設定檔的順序相同
var restartConfigPaths = []string{"config.yaml", "config/config.yaml", "app/config/config.yaml", ".hyp/config.yaml"}

// restartEnvKey 覆寫 server.enable_graceful_restart 的環境變數（config.ApplyEnv 的命名規則）
const restartEnvKey = "HYPGO_SERVER_ENABLE_GRACEFUL_RESTART"

// gracefulRestartEnabled 判斷應用程式是否啟用了 graceful restart；why 說明依據。
// 沒寫就是沒啟用（預設值是 false）。先前只在「有寫且為 false」時才擋：設定檔沒有這個鍵、
// 設定檔不在 config/config.yaml、或以環境變數關閉時照送 SIGUSR2，而沒啟用的 server 不接這個訊號，
// 訊號的預設動作是結束行程——服務直接中斷
func gracefulRestartEnabled() (enabled bool, why string) {
	// 空字串視同沒設定，與 config.ApplyEnv 一致
	if raw := strings.TrimSpace(os.Getenv(restartEnvKey)); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			return b, fmt.Sprintf("%s=%s", restartEnvKey, raw)
		}
	}
	for _, path := range restartConfigPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg struct {
			Server struct {
				EnableGracefulRestart *bool `yaml:"enable_graceful_restart"`
			} `yaml:"server"`
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return false, fmt.Sprintf("%s cannot be parsed: %v", path, err)
		}
		if cfg.Server.EnableGracefulRestart == nil {
			return false, fmt.Sprintf("%s does not set server.enable_graceful_restart (the default is false)", path)
		}
		return *cfg.Server.EnableGracefulRestart, fmt.Sprintf("server.enable_graceful_restart is %v in %s", *cfg.Server.EnableGracefulRestart, path)
	}
	return false, "no config file found in the current directory"
}

// checkGracefulRestart 沒啟用時回錯；force 用於在程式裡啟用、設定檔看不出來的應用程式
func checkGracefulRestart(pid int, force bool) error {
	if force {
		return nil
	}
	if enabled, why := gracefulRestartEnabled(); !enabled {
		return fmt.Errorf("graceful restart is not enabled: %s. Without it the server does not handle SIGUSR2 and the signal would terminate PID %d instead of restarting it. Set server.enable_graceful_restart: true and start the app again, or pass --force if the app enables it in code", why, pid)
	}
	return nil
}

// sameDir 兩個路徑是否為同一個目錄（解開符號連結後比較）
func sameDir(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
