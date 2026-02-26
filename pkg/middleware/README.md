# Middleware Package (`pkg/middleware`)

`middleware` 套件提供了一系列針對 HypGo `context.Context` 量身打造的即插即用（Plug-and-play）中介軟體。透過內置的中間件鏈與群組管理功能，你可以非常輕鬆地將跨域請求、日誌記錄、限流機制以及快取等功能加諸於路由之上。

## 內建中間件列表

1. **Logger**: 高效能的請求日誌記錄，支援記錄耗時與回應大小，甚至可排除特定路徑。
2. **Recovery**: 自動攔截 Panic 並避免伺服器崩潰，可自定義 Panic 處理邏輯與 HTTP 狀態回應。
3. **CORS**: 跨來源資源共用 (Cross-Origin Resource Sharing) 處理，支援各種細部配置與 Preflight 請求快取。
4. **Security**: 提供基礎的 HTTP 安全標頭設定（如 HSTS, X-Frame-Options, Content-Security-Policy）。
5. **RateLimiter**: 提供簡易基於 Token Bucket 的基礎限流機制，可依據 IP 或是自定義規則設限。
6. **Timeout**: 提供 HTTP 請求處理的最大時間限制，超過強制中斷並回應超時訊息。
7. **Cache**: 將經常存取的 API 快取於記憶體中，提供自定義 TTL、Key 產生演算法與驗證邏輯。

## 核心設計理念

HypGo 採用 `Chain` 與 `Group` 模式進行中間件管理，你可以把一個或多個中間件像樂高一樣組合起來，然後透過 `Then` 附加到最終的 Handler 上面。

### 簡單載入單個內建中間件

以 Logger 為例，使用預設配置掛載：

```go
package main

import (
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/context"
)

func main() {
	r := router.New()
	
	// 使用預設設定加上 Logger 中介軟體
	r.Use(middleware.Logger(middleware.LoggerConfig{}))
	
	r.GET("/ping", func(c *context.Context) {
		c.String(200, "pong")
	})
}
```

### 多個中間件串聯 (Chain)

你也可以透過 `middleware.Chain` 將中間件先綑綁成一個群組再服用：

```go
// 建立一個基礎的安全呼叫鏈
apiChain := middleware.NewChain(
    middleware.Recovery(middleware.RecoveryConfig{}), // 防止崩潰
    middleware.Logger(middleware.LoggerConfig{}),     // 日誌記錄
    middleware.CORS(middleware.CORSConfig{
        AllowOrigins: []string{"https://example.com"},
    }),
)

// 結合自訂函數
r.GET("/api/data", apiChain.Then(func(c *context.Context) {
    c.JSON(200, map[string]string{"msg": "secure data"})
}))
```

### 建立前綴中間件群組 (Group)

如果你的特定路由前綴（如 `/admin`）需要特定的中間件邏輯：

```go
// 宣告一個以 `/admin` 開頭的群組，並附加 RateLimiter
adminGroup := middleware.NewGroup("/admin", middleware.RateLimiter(middleware.RateLimiterConfig{
    Rate:  10, // 每秒 10 個請求
    Burst: 20, // 瞬間最大容量 20
}))

// 為特定的路由套用該群組的中介軟體
r.GET("/admin/dashboard", adminGroup.Handle(func(c *context.Context) {
    c.String(200, "Admin View")
}))
```

## 自製中間件

你隨時可以依照 `func(*hypcontext.Context)` 這個型別，寫出自己的通用中間件。只要確保在邏輯的最後執行 `c.Next()` 以順利推進執行鏈：

```go
func MyCustomMiddleware() hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		// 1. 在處理請求前執行的邏輯
		c.Set("my_key", "my_value")

		// 2. 將控制權交給下一個中介軟體或是 Handler
		c.Next()

		// 3. 在處理結束準備回應前執行的邏輯
		status := c.Writer.Status()
		log.Printf("Response Status: %d", status)
	}
}
```
