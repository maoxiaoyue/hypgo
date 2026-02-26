# Config Package (`pkg/config`)

`config` 套件提供一套靈活、強大且結構化的配置管理方案，專為 HypGo 打造。支援讀取 YAML 設定檔、環境變數擴充、參數驗證，以及物件預設值的處理。

## 主要功能

- **支援 YAML 與反序列化**: 透過 `mapstructure` 以及 `yaml` 標籤，可輕鬆地將設定檔映射到結構體。
- **靈活配置來源**: `LoadConfig` 方法允許從給定路徑讀取設定檔並載入到應用程式中；如果該路徑沒找到配置，將拋出錯誤。
- **介面隔離設計**: 定義了一系列介面（如 `ConfigInterface`, `ServerConfigInterface`, `DatabaseConfigInterface`, `RedisConfigInterface`, `LoggerConfigInterface`），讓配置物件的使用能有明確的約束並提高可測試性。
- **支援讀寫分離配置**: 提供 `ReplicaConfigProvider` 以及 `DatabaseConfig.Replicas` 配置，輕鬆設定多部讀取副本（Read Replicas），也可輕易退回主庫（Primary）。
- **進階驗證支援**: 整合 `@maoxiaoyue/hypgo/pkg/json` 內的校驗工具或其他校驗策略，驗證設定檔配置是否必填或符合規定格式。

## 基礎使用

以下範例會示範如何載入一個名為 `config.yaml` 的配置檔案：

```go
package main

import (
	"log"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

func main() {
	// 假設有 config.yaml
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("無法讀取配置: %v", err)
	}

	// 確保設定的數值都有預設值墊底
	cfg.ApplyDefaults()

	log.Printf("Server 將啟動於 %s", cfg.GetServerConfig().GetAddr())
}
```

## 配置結構

配置主要分為三大區塊：

1. **Server**: 設定伺服器連線、Protocol (HTTP2/3 等)、TLS，以及 Graceful Restart 等。
2. **Database**: 定義連線驅動、DSN、連線池設定，甚至是 Redis 與多部 Data Replicas 的設定。
3. **Logger**: 管理輸出的日誌層級、格式、輸出目的地等等。

例如 `config.yaml`：

```yaml
server:
  addr: ":8080"
  protocol: "HTTP/1.1"

database:
  driver: "mysql"
  dsn: "user:pass@tcp(127.0.0.1:3306)/dbname"

logger:
  level: "info"
  format: "json"
```

## 預設值參考

如果沒有提供某些參數，呼叫 `cfg.ApplyDefaults()` 時預設：
- Server `addr` 為 `:8080`
- Server `protocol` 為 `HTTP/1.1`
- Read/Write Timeout 為 `10` / `10`
- Logger `level` 為 `info`
- DB MaxIdle/Open Conns 等也有內建預設參考
