# Router Package (`pkg/router`)

`router` 套件為 HypGo 實現了核心的路由分發機制。它基於高效的基數樹（Radix Tree）實作，擁有極快的路由比對速度，支援動態參數擷取與萬用字元配對，同時將路由群組管理整合在內，大幅簡化大型應用的 API 結構組織。

## 主要特色

- **零配置的高效能 Radix Tree**: 自動化建立前綴樹進行路由檢索，執行速度遠勝一般基於 Regular Expression 的配對方式。
- **支援 RESTful 參數與萬用字元**: 如 `:id`（名稱配對）、`*filepath`（萬用配對）。
- **內建記憶體快取 (LRU Route Cache)**: 若啟動時啟用，可以快取頻繁造訪的路由解析結果，使極高併發的路由比對效能再提升一個量級。
- **靈活的中間件與群組 (Group)**: 透過路由群組管理特定前綴的路由，並且能夠為全域或是特定的 Group 插入中介軟體（Middleware）。
- **嚴格或寬鬆的斜線處理 (Strict Slash)**: 可配置是否將 `/path` 與 `/path/` 視作同一路徑並自動重新導向。
- **無縫整合 HTTP/3**: 提供 `EnableHTTP3` 機制讓 HTTP/3 與 QUIC 原生整合進路由的支援中。

## 基礎使用

初始化一個路由器並加入簡單的路徑：

```go
package main

import (
	"github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

func main() {
	// 使用預設配置建立路由器
	r := router.New()

	// 基礎的 GET / POST
	r.GET("/ping", func(c *context.Context) {
		c.String(200, "pong")
	})

	r.POST("/submit", func(c *context.Context) {
		c.String(200, "Submitted!")
	})
	
	// 在 main 裡面搭配 Http Server 啟動 (由 pkg/server 負責)
}
```

## 路由參數配對

存取由 `:name` 或 `*action` 定義的路徑參數：

```go
// 匹配 /user/alice, /user/bob 等
r.GET("/user/:name", func(c *context.Context) {
	name := c.Param("name")
	c.String(200, "Hello %s", name)
})

// 匹配 /files/js/main.js 或 /files/css/style.css
r.GET("/files/*filepath", func(c *context.Context) {
	path := c.Param("filepath")
	c.String(200, "Requested file path: %s", path)
})
```

## 路由群組 (Group)

透過路由群組，能很方便的將不同業務邏輯切割開來：

```go
api := r.NewGroup("/api/v1")
{
	users := api.NewGroup("/users")
	{
		users.GET("/", listUsers)        // /api/v1/users/
		users.GET("/:id", getUser)       // /api/v1/users/:id
		users.POST("/", createUser)      // /api/v1/users/
	}

	orders := api.NewGroup("/orders")
	{
		orders.GET("/", listOrders)      // /api/v1/orders/
		orders.GET("/:id", getOrder)     // /api/v1/orders/:id
	}
}
```

## 全域與群組中間件

使用 `Use` 可以在不同層級掛載 `hypcontext.HandlerFunc`：

```go
// 全域套用：所有經過此路由器的請求都會先經過 Middleware1, Middleware2
r.Use(Middleware1, Middleware2)

// 單獨掛載：僅掛載於 api 群組
api := r.NewGroup("/api/v1")
api.Use(AuthMiddleware)
api.GET("/secure", secureHandler) // 必須經過 AuthMiddleware 才會執行
```

## 客製化配置

`router.New` 也開放以函數式選項進行詳細設定：

```go
opts := []router.RouterOption{
    router.WithCache(1000),             // 啟用 LRU Route Cache，快取 1000 條結果
    router.WithStrictSlash(true),       // 嚴格區分結尾斜線
    router.WithMethodNotAllowed(true),  // 自動捕捉並回應 405 Method Not Allowed
}
r := router.New(opts...)
```
