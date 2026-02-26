# Database Package (`pkg/database`)

`database` 套件為 HypGo 提供強大、靈活的資料庫連接與管理功能，基於 [Bun](https://bun.uptrace.dev/)（一個對開發者友善的 Go ORM）與 Redis 所建構。它支援多種資料庫後端、自動讀寫分離機制以及可擴展的插件生態系統。

## 主要特色

- **多 Driver 支援**: 原生支援 MySQL（相容 TiDB）與 PostgreSQL。
- **基於 Bun ORM**: 透過 Bun 封裝 `*sql.DB` 獲得極快的執行效能與符合 SQL 語意的方法。
- **整合 Redis**: 只要有設定 Redis，呼叫 `New` 或 `NewWithInterface` 就會同時建立 `*redis.Client` 連線。
- **讀寫分離 (Read/Write Splitting)**: 原生支援讀取副本（Read Replicas）。`DB()` 預設回傳主庫適合寫入，而 `ReadDB()` 讓你在負載均衡中隨機取得一個讀取副本以執行唯讀查詢，若未設定讀取副本則會自動無縫退回使用主庫。
- **全方位健康檢查**: 內建 `HealthCheck()` 方法，只需一行指令即可同時驗證主庫、所有讀取副本、Redis 乃至各個載入的系統外掛（Plugins）其可用度。
- **外掛機制 (Plugins)**: 定義 `DatabasePlugin` 介面，讓你可以建立擴充模組，並且在系統啟動時與資料庫同時連接並受健康檢查控管。

## 基礎使用

使用 `config` 的設定來初始化 `Database`：

```go
package repository

import (
	"context"
	"log"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/database"
)

func main() {
	cfg, _ := config.LoadConfig("config.yaml")

	// 初始化資料庫連線
	db, err := database.NewWithInterface(cfg.GetDatabaseConfig())
	if err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}
	defer db.Close() // 程式結束時關閉所有連線

	// 確保一切連線正常
	if err := db.HealthCheck(context.Background()); err != nil {
		log.Fatalf("健康檢查未通過: %v", err)
	}

	// 寫入資料（使用主庫）
	user := &User{Name: "Alice"}
	_, err = db.DB().NewInsert().Model(user).Exec(context.Background())

	// 讀取資料（使用讀取庫）
	users := make([]User, 0)
	err = db.ReadDB().NewSelect().Model(&users).Scan(context.Background())

	// 使用 Redis
	if db.Redis() != nil {
		db.Redis().Set(context.Background(), "my_key", "value", 0)
	}
}
```

## 關於讀寫分離的配置

在 `config.yaml` 裡，只要加上 `replicas` 的設定，`Database` 元件便會自動啟用讀寫分離功能：

```yaml
database:
  driver: "mysql"
  dsn: "user:pass@tcp(master:3306)/dbname"
  replicas:
    - dsn: "user:pass@tcp(replica1:3306)/dbname"
    - dsn: "user:pass@tcp(replica2:3306)/dbname"
```
搭配 `db.ReadDB()` 的隨機選取，輕鬆降低主庫的壓力。
