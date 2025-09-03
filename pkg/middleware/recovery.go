package middleware

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ===== Recovery 中間件 =====

// RecoveryConfig Recovery 配置
type RecoveryConfig struct {
	StackSize         int
	DisableStackAll   bool
	DisablePrintStack bool
	LogLevel          string
	ErrorHandler      func(c *hypcontext.Context, err interface{})
}

// Recovery 創建錯誤恢復中間件
func Recovery(config RecoveryConfig) hypcontext.HandlerFunc {
	if config.StackSize == 0 {
		config.StackSize = 4 << 10 // 4KB
	}

	return func(c *hypcontext.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 獲取堆疊資訊
				stack := make([]byte, config.StackSize)
				length := runtime.Stack(stack, !config.DisableStackAll)
				stack = stack[:length]

				// 記錄錯誤
				if !config.DisablePrintStack {
					fmt.Printf("[Recovery] panic recovered:\n%s\n%s\n", err, stack)
				}

				// HTTP/3 特定處理：確保流正確關閉
				if protoValue, exists := c.Get("protocol"); exists {
					if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
						// 關閉 QUIC 流
						// 這裡需要實際的流關閉邏輯
					}
				}

				// 執行自定義錯誤處理器
				if config.ErrorHandler != nil {
					config.ErrorHandler(c, err)
				} else {
					c.AbortWithStatus(http.StatusInternalServerError)
				}
			}
		}()

		c.Next()
	}
}

// ===== 重試中間件（HTTP/3 優化）=====

// RetryConfig 重試配置
type RetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
	MaxDelay    time.Duration
	BackoffFunc func(attempt int) time.Duration
	ShouldRetry func(c *hypcontext.Context, err error) bool
}

// Retry 創建重試中間件（針對 HTTP/3 優化）
func Retry(config RetryConfig) hypcontext.HandlerFunc {
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}

	if config.Delay == 0 {
		config.Delay = 100 * time.Millisecond
	}

	if config.MaxDelay == 0 {
		config.MaxDelay = 5 * time.Second
	}

	if config.BackoffFunc == nil {
		config.BackoffFunc = func(attempt int) time.Duration {
			delay := config.Delay * time.Duration(1<<uint(attempt))
			if delay > config.MaxDelay {
				return config.MaxDelay
			}
			return delay
		}
	}

	return func(c *hypcontext.Context) {
		var lastErr error

		for attempt := 0; attempt < config.MaxAttempts; attempt++ {
			// 執行請求
			err := executeWithRecovery(c)

			if err == nil {
				// 成功
				return
			}

			lastErr = err

			// 檢查是否應該重試
			if config.ShouldRetry != nil && !config.ShouldRetry(c, err) {
				break
			}

			// HTTP/3 優化：根據 RTT 調整延遲
			delay := config.BackoffFunc(attempt)
			if protoValue, exists := c.Get("protocol"); exists {
				if proto, ok := protoValue.(string); ok && proto == "HTTP/3" {
					rtt := c.GetRTT()
					if rtt > 0 {
						// 根據網路延遲調整重試延遲
						delay = delay + rtt
					}
				}
			}

			// 等待後重試
			if attempt < config.MaxAttempts-1 {
				time.Sleep(delay)
			}
		}

		// 所有重試都失敗
		if lastErr != nil {
			c.Error(lastErr)
			c.AbortWithStatus(http.StatusServiceUnavailable)
		}
	}
}

// executeWithRecovery 執行請求並捕獲錯誤
func executeWithRecovery(c *hypcontext.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	c.Next()

	// 檢查是否有錯誤
	if c.Errors != nil && len(c.Errors) > 0 {
		return fmt.Errorf("request error occurred")
	}

	// 檢查狀態碼
	status := c.Response.Status()
	if status >= 500 {
		return fmt.Errorf("server error: %d", status)
	}

	return nil
}

// ===== 熔斷器中間件 =====

// CircuitBreakerConfig 熔斷器配置
type CircuitBreakerConfig struct {
	FailureThreshold   int           // 失敗次數閾值
	SuccessThreshold   int           // 成功次數閾值（用於恢復）
	Timeout            time.Duration // 熔斷器打開後的超時時間
	MaxConcurrentCalls int           // 最大並發請求數
	OnStateChange      func(from, to string)
}

// CircuitBreakerState 熔斷器狀態
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker 創建熔斷器中間件
func CircuitBreaker(config CircuitBreakerConfig) hypcontext.HandlerFunc {
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold == 0 {
		config.SuccessThreshold = 2
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	state := StateClosed
	failureCount := 0
	successCount := 0
	lastFailureTime := time.Time{}
	concurrentCalls := 0

	return func(c *hypcontext.Context) {
		// 檢查熔斷器狀態
		switch state {
		case StateOpen:
			// 檢查是否應該進入半開狀態
			if time.Since(lastFailureTime) > config.Timeout {
				state = StateHalfOpen
				failureCount = 0
				successCount = 0
				if config.OnStateChange != nil {
					config.OnStateChange("open", "half-open")
				}
			} else {
				// 快速失敗
				c.AbortWithStatus(http.StatusServiceUnavailable)
				c.String(http.StatusServiceUnavailable, "Circuit breaker is open")
				return
			}

		case StateHalfOpen:
			// 限制並發請求
			if config.MaxConcurrentCalls > 0 && concurrentCalls >= config.MaxConcurrentCalls {
				c.AbortWithStatus(http.StatusServiceUnavailable)
				c.String(http.StatusServiceUnavailable, "Too many concurrent requests")
				return
			}
		}

		// 執行請求
		concurrentCalls++
		defer func() { concurrentCalls-- }()

		// 記錄開始時間
		startTime := time.Now()

		// 執行下一個處理器
		c.Next()

		// 記錄結果
		elapsed := time.Since(startTime)
		statusCode := c.Response.Status()

		// 判斷請求是否成功
		isSuccess := statusCode < 500

		// 更新熔斷器狀態
		switch state {
		case StateClosed:
			if !isSuccess {
				failureCount++
				lastFailureTime = time.Now()
				if failureCount >= config.FailureThreshold {
					state = StateOpen
					if config.OnStateChange != nil {
						config.OnStateChange("closed", "open")
					}
				}
			} else {
				failureCount = 0
			}

		case StateHalfOpen:
			if isSuccess {
				successCount++
				if successCount >= config.SuccessThreshold {
					state = StateClosed
					failureCount = 0
					if config.OnStateChange != nil {
						config.OnStateChange("half-open", "closed")
					}
				}
			} else {
				state = StateOpen
				lastFailureTime = time.Now()
				if config.OnStateChange != nil {
					config.OnStateChange("half-open", "open")
				}
			}
		}

		// 記錄指標
		c.Set("circuit_breaker_state", state)
		c.Set("request_elapsed", elapsed)
	}
}

// ===== 錯誤處理中間件 =====

// ErrorHandlerConfig 錯誤處理配置
type ErrorHandlerConfig struct {
	LogErrors         bool
	SendDetailedError bool
	ErrorHandler      func(c *hypcontext.Context, err error)
	ErrorMap          map[error]int // 錯誤到狀態碼的映射
}

// ErrorHandler 創建錯誤處理中間件
func ErrorHandler(config ErrorHandlerConfig) hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		defer func() {
			if len(c.Errors) > 0 {
				err := c.Errors[0]

				// 記錄錯誤
				if config.LogErrors {
					fmt.Printf("[Error] %s %s: %v\n",
						c.Request.Method,
						c.Request.URL.Path,
						err)
				}

				// 自定義錯誤處理
				if config.ErrorHandler != nil {
					config.ErrorHandler(c, err)
					return
				}

				// 根據錯誤類型設置狀態碼
				statusCode := http.StatusInternalServerError
				if config.ErrorMap != nil {
					if code, ok := config.ErrorMap[err]; ok {
						statusCode = code
					}
				}

				// 返回錯誤響應
				if config.SendDetailedError {
					c.JSON(statusCode, map[string]interface{}{
						"error":     err.Error(),
						"path":      c.Request.URL.Path,
						"method":    c.Request.Method,
						"timestamp": time.Now().Unix(),
					})
				} else {
					c.JSON(statusCode, map[string]string{
						"error": "Internal Server Error",
					})
				}
			}
		}()

		c.Next()
	}
}
