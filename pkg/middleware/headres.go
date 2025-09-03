package middleware

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== 壓縮中間件 =====

// CompressionConfig 壓縮配置
type CompressionConfig struct {
	Level         int
	MinLength     int
	ExcludedPaths []string
	ExcludedTypes []string
	PreferBrotli  bool // HTTP/3 優化：優先使用 Brotli
}

// Compression 創建壓縮中間件
func Compression(config CompressionConfig) hypcontext.HandlerFunc {
	if config.Level == 0 {
		config.Level = gzip.DefaultCompression
	}

	if config.MinLength == 0 {
		config.MinLength = 1024
	}

	excludedPaths := make(map[string]bool)
	for _, path := range config.ExcludedPaths {
		excludedPaths[path] = true
	}

	return func(c *hypcontext.Context) {
		if excludedPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 檢查客戶端支援的編碼
		acceptEncoding := c.GetHeader("Accept-Encoding")

		// HTTP/3 優化：優先使用 Brotli
		if config.PreferBrotli {
			if protoValue, exists := c.Get("protocol"); exists {
				if proto, ok := protoValue.(string); ok && proto == "HTTP/3" && strings.Contains(acceptEncoding, "br") {
					// 使用 Brotli 壓縮
					c.Header("Content-Encoding", "br")
					// 實現 Brotli 壓縮邏輯
				}
			}
		} else if strings.Contains(acceptEncoding, "gzip") {
			// 使用 Gzip 壓縮
			c.Header("Content-Encoding", "gzip")
			c.Header("Vary", "Accept-Encoding")

			gz := gzip.NewWriter(c.Response)
			defer gz.Close()

			c.Response = &gzipWriter{ResponseWriter: c.Response, Writer: gz}
		}

		c.Next()
	}
}

// gzipWriter 包裝 ResponseWriter 以支援 gzip
type gzipWriter struct {
	hypcontext.ResponseWriter
	io.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.Writer.Write(data)
}

// ===== 請求 ID 中間件 =====

// RequestIDConfig 請求 ID 配置
type RequestIDConfig struct {
	Header    string
	Generator func() string
}

// RequestID 創建請求 ID 中間件
func RequestID(config RequestIDConfig) hypcontext.HandlerFunc {
	if config.Header == "" {
		config.Header = "X-Request-ID"
	}

	if config.Generator == nil {
		config.Generator = generateRequestID
	}

	return func(c *hypcontext.Context) {
		// 檢查是否已有請求 ID
		requestID := c.GetHeader(config.Header)
		if requestID == "" {
			requestID = config.Generator()
		}

		// 設置請求 ID
		c.Set("request_id", requestID)
		c.Header(config.Header, requestID)

		c.Next()
	}
}

// generateRequestID 生成請求 ID
func generateRequestID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), fastrand())
}

// ===== HTTP/3 Server Push 中間件 =====

// ServerPushConfig Server Push 配置
type ServerPushConfig struct {
	Rules []PushRule
}

// PushRule Server Push 規則
type PushRule struct {
	Path      string
	Resources []string
	Condition func(c *hypcontext.Context) bool
}

// ServerPush 創建 Server Push 中間件
func ServerPush(config ServerPushConfig) hypcontext.HandlerFunc {
	// 預編譯路徑匹配
	rules := make(map[string][]string)
	conditions := make(map[string]func(c *hypcontext.Context) bool)

	for _, rule := range config.Rules {
		rules[rule.Path] = rule.Resources
		if rule.Condition != nil {
			conditions[rule.Path] = rule.Condition
		}
	}

	return func(c *hypcontext.Context) {
		// 只在 HTTP/2 和 HTTP/3 中啟用
		if protoValue, exists := c.Get("protocol"); exists {
			if proto, ok := protoValue.(string); ok {
				if proto != "HTTP/2" && proto != "HTTP/3" {
					c.Next()
					return
				}
			}
		}

		path := c.Request.URL.Path

		// 查找匹配的規則
		if resources, ok := rules[path]; ok {
			// 檢查條件
			if condition, hasCondition := conditions[path]; hasCondition {
				if !condition(c) {
					c.Next()
					return
				}
			}

			// 推送資源
			for _, resource := range resources {
				if err := c.Push(resource, nil); err != nil {
					// 記錄推送失敗
					fmt.Printf("Failed to push %s: %v\n", resource, err)
				}
			}
		}

		c.Next()
	}
}

// ===== ETag 中間件 =====

// ETagConfig ETag 配置
type ETagConfig struct {
	Weak      bool
	Generator func(c *hypcontext.Context, body []byte) string
}

// ETag 創建 ETag 中間件
func ETag(config ETagConfig) hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		// 只處理 GET 和 HEAD 請求
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}

		// 檢查客戶端 ETag
		clientETag := c.GetHeader("If-None-Match")

		// 處理請求並生成 ETag
		c.Next()

		// 生成 ETag
		var etag string
		if config.Generator != nil {
			// 使用自定義生成器
			// 這裡需要捕獲響應體來生成 ETag
		} else {
			// 使用默認生成器
			etag = fmt.Sprintf(`"%d-%d"`, c.Response.Status(), c.Response.Size())
		}

		if config.Weak {
			etag = "W/" + etag
		}

		// 設置 ETag 頭
		c.Header("ETag", etag)

		// 如果 ETag 匹配，返回 304
		if clientETag == etag {
			c.AbortWithStatus(http.StatusNotModified)
		}
	}
}

// ===== 條件請求中間件 =====

// ConditionalConfig 條件請求配置
type ConditionalConfig struct {
	SkipPaths []string
}

// Conditional 創建條件請求中間件
func Conditional(config ConditionalConfig) hypcontext.HandlerFunc {
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}

	return func(c *hypcontext.Context) {
		if skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 檢查 If-Modified-Since
		ifModifiedSince := c.GetHeader("If-Modified-Since")
		if ifModifiedSince != "" {
			// 解析時間
			t, err := time.Parse(http.TimeFormat, ifModifiedSince)
			if err == nil {
				// 設置到上下文
				c.Set("if_modified_since", t)
			}
		}

		// 檢查 If-Unmodified-Since
		ifUnmodifiedSince := c.GetHeader("If-Unmodified-Since")
		if ifUnmodifiedSince != "" {
			t, err := time.Parse(http.TimeFormat, ifUnmodifiedSince)
			if err == nil {
				c.Set("if_unmodified_since", t)
			}
		}

		// 檢查 If-Match
		ifMatch := c.GetHeader("If-Match")
		if ifMatch != "" {
			c.Set("if_match", ifMatch)
		}

		// 檢查 If-None-Match
		ifNoneMatch := c.GetHeader("If-None-Match")
		if ifNoneMatch != "" {
			c.Set("if_none_match", ifNoneMatch)
		}

		c.Next()
	}
}
