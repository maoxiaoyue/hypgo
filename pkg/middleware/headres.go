// @chris
package middleware

import (
	"compress/gzip"
	"io"
	"strings"

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

		// 壓縮：僅支援 gzip（Brotli 需要額外依賴，暫不啟用避免發送假 header）
		if strings.Contains(acceptEncoding, "gzip") {
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

// generateRequestID 生成安全的請求 ID
func generateRequestID() string {
	return secureRandHex(16)
}

