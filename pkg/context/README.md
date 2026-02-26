# Context Package (`pkg/context`)

`context` 套件是 HypGo 框架的核心，提供跨協議（HTTP/1.1, HTTP/2, HTTP/3）一致的上下文封裝。透過它，你可以存取請求資訊、產生各種格式的回應（JSON、XML、HTML 等），以及在中間件（Middleware）之間傳遞狀態。

## 主要功能

- **標準庫兼容**: 提供方法將 HypGo 的 `*Context` 封裝進 `context.Context`，又或是取出。適合在框架之外呼叫需要標準 `context.Context` 的第三方函式庫。
- **統一的回應介面**: 提供多種簡便方法如 `JSON()`, `XML()`, `String()`, `HTML()` 以及 `File()` 以輸出不同格式的內容。
- **內建協商（Content-Negotiation）**: `Negotiate()` 可根據客戶端的 `Accept` 標頭自動決定回應 JSON, XML, HTML 等合適格式。
- **物件池（Object Pooling）**: 為了達到極致效能，`Context`、`ResponseWriter` 等頻繁建立的物件全面導入 `sync.Pool` 管理，大幅減少 GC 壓力。
- **中間件支援**: 提供 `Next()`, `Abort()`, `AbortWithStatus()` 等控制流程的方法，讓你可以輕鬆組建洋蔥式（Onion-like）中介軟體。
- **QUIC / HTTP/3 指標提取**: 若為 HTTP/3 連線，可以直接透過 `Context` 取得 RTT (Round Trip Time) 與底層連線資訊。

## 基礎使用

在 Controller 中使用 `hypcontext.Context`：

```go
package user

import (
	"net/http"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func GetUser(c *hypcontext.Context) {
	// 從網址列取得變數 (例如 /user/:id)
	id := c.Param("id")

	// 從查詢參數取得資訊 (例如 ?lang=zh)
	lang := c.DefaultQuery("lang", "en")

	// 在上下文中傳遞值 (可用於 Middleware)
	c.Set("user_id", id)

	// 回傳 JSON
	c.JSON(http.StatusOK, map[string]interface{}{
		"id":   id,
		"lang": lang,
		"status": "active",
	})
}
```

## 中間件控制

使用 `Next()` 與 `Abort()` 來控制請求流程：

```go
func AuthMiddleware(c *hypcontext.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]string{
			"error": "Missing token",
		})
		return // 必須 return，停止當前 func 執行
	}

	// 繼續執行下一個 handler
	c.Next()
}
```

## 在 Goroutine 內使用

如果你需要在另一個 Goroutine 繼續存取 `Context` 的內容，請記得呼叫 `c.Copy()` 產生副本，因為原本的 Context 在請求結束時會丟回物件池並清空。

```go
func AsyncProcess(c *hypcontext.Context) {
	// 產生唯讀副本
	cCopy := c.Copy()

	go func() {
		// 在這個 Goroutine 只能使用 cCopy，不可使用原本的 c
		DoSomething(cCopy.Request.URL.Path)
	}()

	c.String(200, "Background task started")
}
```
