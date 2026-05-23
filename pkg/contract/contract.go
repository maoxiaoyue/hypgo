// Package contract 提供 Contract Testing 內建驗證功能
// 根據 schema-first 路由的 metadata 自動驗證 handler 的行為
// 確保 AI 生成的程式碼符合宣告的合約
package contract

// @ai purpose: Contract Testing 核心 — Test / TestAll / TestRoute 三個公開測試函式
// @ai input: *testing.T、*router.Router、TestCase / RunConfig
// @ai output: 無（透過 t.Errorf / t.Skip 回報結果）
// @ai sideeffect: 呼叫 router.ServeHTTP（httptest 模式，不開真實 port）
// date: 2026-05-23

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/eval"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// TestCase 定義單一 contract 測試案例
type TestCase struct {
	// Route 格式為 "METHOD /path"，例如 "POST /api/users"
	Route string

	// Input 為 JSON 格式的請求 body
	Input string

	// Headers 為自訂請求標頭
	Headers map[string]string

	// Query 為 URL query 參數
	Query map[string]string

	// ExpectStatus 為期望的 HTTP 狀態碼
	ExpectStatus int

	// ExpectSchema 為 true 時，驗證回應 body 是否符合 Output schema
	ExpectSchema bool

	// ExpectBody 為非空時，精確比對回應 body
	ExpectBody string

	// schemaPath 為內部欄位，用於 TestAll 時保留原始 schema 路徑（含 :param）
	schemaPath string
}

// Test 執行單一 contract 測試
//
// @ai purpose: 手動執行單一路由的 contract 驗證，適合邊界條件與錯誤路徑測試
// @ai input: *testing.T、*router.Router、TestCase（完整定義）
// @ai output: 無
// @ai sideeffect: 呼叫 t.Errorf / t.Fatalf
// date: 2026-05-23
func Test(t *testing.T, r *router.Router, tc TestCase) {
	t.Helper()
	testInternal(t, r, tc)
}

// runTestOnce 執行一次 contract 測試並回傳結果，不接觸 *testing.T
// 這是純執行函式，讓重試迴圈可以多次呼叫而不污染 t 的狀態
//
// @ai purpose: 將 HTTP 執行邏輯與 t.Errorf 解耦，支援 TestAll 的重試機制
// @ai input: *router.Router（被測路由器）、TestCase（測試定義）
// @ai output: pass bool（是否通過）、reason string（失敗原因，pass=true 時為空字串）
// @ai sideeffect: 無（不呼叫任何 t.* 方法）
// date: 2026-05-23
func runTestOnce(r *router.Router, tc TestCase) (pass bool, reason string) {
	method, path := parseRoute(tc.Route)
	if method == "" || path == "" {
		return false, fmt.Sprintf("invalid route format: %q (expected \"METHOD /path\")", tc.Route)
	}

	// 建立請求 body
	var bodyStr string
	if tc.Input != "" {
		bodyStr = tc.Input
	}
	req := httptest.NewRequest(method, path, strings.NewReader(bodyStr))
	if tc.Input != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 設定自訂標頭
	for k, v := range tc.Headers {
		req.Header.Set(k, v)
	}

	// 設定 query 參數
	if len(tc.Query) > 0 {
		q := req.URL.Query()
		for k, v := range tc.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// 執行請求
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 驗證狀態碼
	if tc.ExpectStatus > 0 && w.Code != tc.ExpectStatus {
		return false, fmt.Sprintf("[%s] status = %d, want %d", tc.Route, w.Code, tc.ExpectStatus)
	}

	// 驗證 schema（使用 schemaPath 或 URL path 查找）
	if tc.ExpectSchema {
		lookupPath := path
		if tc.schemaPath != "" {
			lookupPath = tc.schemaPath
		}
		s, ok := schema.Global().Get(method, lookupPath)
		if !ok {
			return false, fmt.Sprintf("[%s] no schema registered for validation", tc.Route)
		}
		// 驗證 Input schema（請求 body）
		if s.Input != nil && tc.Input != "" {
			if err := validateRequest([]byte(tc.Input), s.Input); err != nil {
				return false, fmt.Sprintf("[%s] request schema validation failed: %v", tc.Route, err)
			}
		}
		// 驗證 Output schema（回應 body）
		if s.Output != nil {
			if err := validateResponse(w.Body.Bytes(), s.Output); err != nil {
				return false, fmt.Sprintf("[%s] response schema validation failed: %v", tc.Route, err)
			}
		}
	}

	// 精確比對 body
	if tc.ExpectBody != "" {
		got := strings.TrimSpace(w.Body.String())
		want := strings.TrimSpace(tc.ExpectBody)
		if got != want {
			return false, fmt.Sprintf("[%s] body = %q, want %q", tc.Route, got, want)
		}
	}

	return true, ""
}

// testInternal 為 Test() 的內部共用實作
// 呼叫 runTestOnce 並將失敗結果報告至 t.Errorf
//
// @ai purpose: 橋接純執行函式（runTestOnce）與 testing.T 回報機制
// @ai input: *testing.T、*router.Router、TestCase
// @ai output: 無
// @ai sideeffect: 呼叫 t.Errorf / t.Fatalf
// date: 2026-05-23
func testInternal(t *testing.T, r *router.Router, tc TestCase) {
	t.Helper()
	pass, reason := runTestOnce(r, tc)
	if !pass {
		t.Errorf("contract: %s", reason)
	}
}

// TestAll 自動測試所有 schema-registered 路由
// REST 路由：發送 HTTP 請求並驗證 Input/Output
// 非 REST 路由：驗證 schema 定義完整性（不發送請求）
//
// 可選傳入 RunConfig 啟用並行、重試、速率限制、FailFast 與 History 記錄：
//
//	contract.TestAll(t, r)                              // 向後相容，循序執行
//	contract.TestAll(t, r, contract.RunConfig{
//	    Parallel: true, MaxWorkers: 4, RetryCount: 2,
//	    RecordHistory: true,                            // append 到 .hyp/eval_history.jsonl
//	})
//
// @ai purpose: 一鍵驗證所有 schema-registered 路由的 contract，支援執行引擎設定與歷史記錄
// @ai input: *testing.T、*router.Router、可選的 RunConfig（向後相容）
// @ai output: 無
// @ai sideeffect: 呼叫 t.Run / t.Parallel / t.Errorf / t.Logf / t.Skip；RecordHistory=true 時 append .hyp/eval_history.jsonl
// date: 2026-05-23
func TestAll(t *testing.T, r *router.Router, cfgs ...RunConfig) {
	t.Helper()

	cfg := mergeRunConfig(cfgs)

	routes := schema.Global().All()
	if len(routes) == 0 {
		t.Skip("no schema-registered routes found")
	}

	// FailFast 信號：atomic 確保 goroutine 間無鎖可見
	var aborted atomic.Bool

	// 速率限制器：由所有子測試 goroutine 共享競爭接收（goroutine-safe）
	var limiterCh <-chan time.Time
	if cfg.RateLimit > 0 {
		ticker := time.NewTicker(time.Second / time.Duration(cfg.RateLimit))
		limiterCh = ticker.C
		// t.Cleanup 確保 ticker 在所有並行子測試完成後才停止
		t.Cleanup(ticker.Stop)
	}

	// git commit hash：只在 RecordHistory=true 時查詢，且只查詢一次
	var gitCommit string
	if cfg.RecordHistory {
		gitCommit = eval.GitShortCommit()
	}

	for _, route := range routes {
		route := route // capture for goroutine / closure

		if route.IsREST() {
			name := route.Method + " " + route.Path
			t.Run(name, func(t *testing.T) {
				// FailFast 檢查：並行模式下在 t.Parallel() 後仍需檢查
				if aborted.Load() {
					t.Skip("skipped: FailFast abort")
					return
				}

				if cfg.Parallel {
					t.Parallel()
				}

				// 並行模式下，goroutine 重新啟動後再次檢查
				if aborted.Load() {
					t.Skip("skipped: FailFast abort")
					return
				}

				// 速率限制：所有 goroutine 競爭接收同一個 ticker channel
				if cfg.RateLimit > 0 {
					<-limiterCh
				}

				tc := generateTestCase(route)
				tc.schemaPath = route.Path

				// 重試迴圈（含計時，用於 RecordHistory）
				start := time.Now()
				var pass bool
				var reason string
				for attempt := 0; attempt <= cfg.RetryCount; attempt++ {
					if attempt > 0 {
						t.Logf("[retry %d/%d] %s %s", attempt, cfg.RetryCount,
							route.Method, route.Path)
						if cfg.RetryDelay > 0 {
							time.Sleep(cfg.RetryDelay)
						}
					}
					pass, reason = runTestOnce(r, tc)
					if pass {
						break
					}
				}
				latencyMs := time.Since(start).Milliseconds()

				if !pass {
					t.Errorf("contract: %s", reason)
					if cfg.FailFast {
						aborted.Store(true)
					}
				}

				// 歷史記錄：RecordHistory=true 時 append 結果
				if cfg.RecordHistory {
					score := 0.0
					if pass {
						score = 1.0
					}
					status := "fail"
					if pass {
						status = "pass"
					}
					_ = eval.AppendHistory(eval.EvalRecord{
						Timestamp:  time.Now().UTC(),
						GitCommit:  gitCommit,
						Route:      route.Method + " " + route.Path,
						Status:     status,
						Scores:     map[string]float64{"contract": score},
						LatencyMs:  latencyMs,
						InputHash:  eval.HashInput(tc.Input),
						FailReason: reason,
					})
				}
			})
		} else {
			// 非 REST 路由：驗證 schema 定義完整性
			name := route.Protocol + "|" + route.Command
			t.Run(name, func(t *testing.T) {
				if aborted.Load() {
					t.Skip("skipped: FailFast abort")
					return
				}
				if cfg.Parallel {
					t.Parallel()
				}
				testSchemaCompleteness(t, route)
				// FailFast：testSchemaCompleteness 失敗後設定 aborted
				if t.Failed() && cfg.FailFast {
					aborted.Store(true)
				}
			})
		}
	}
}

// testSchemaCompleteness 驗證非 REST 路由的 schema 定義完整性
// 不發送實際請求，只檢查 schema 本身的品質
//
// @ai purpose: 對 gRPC / Bot / MCP / WebSocket / CLI 路由做 schema 品質檢查
// @ai input: *testing.T、schema.Route（非 REST 路由）
// @ai output: 無
// @ai sideeffect: 呼叫 t.Errorf
// date: 2026-05-23
func testSchemaCompleteness(t *testing.T, route schema.Route) {
	t.Helper()

	label := route.Protocol + "|" + route.Command

	if route.Command == "" {
		t.Errorf("[%s] missing Command", label)
	}
	if route.Summary == "" {
		t.Errorf("[%s] missing Summary", label)
	}
	// Input/Output 不強制要求（有些命令確實沒有 Input 或 Output）
	// 但如果有，型別名稱應該已自動填入
	if route.Input != nil && route.InputName == "" {
		t.Errorf("[%s] has Input but missing InputName", label)
	}
	if route.Output != nil && route.OutputName == "" {
		t.Errorf("[%s] has Output but missing OutputName", label)
	}
}

// TestRoute 測試單一路由是否已註冊且可回應
//
// @ai purpose: 簡易路由存在性驗證，不需要 schema 宣告
// @ai input: *testing.T、*router.Router、method string、path string、expectStatus int
// @ai output: 無
// @ai sideeffect: 呼叫 t.Errorf
// date: 2026-05-23
func TestRoute(t *testing.T, r *router.Router, method, path string, expectStatus int) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != expectStatus {
		t.Errorf("%s %s: status = %d, want %d", method, path, w.Code, expectStatus)
	}
}

// parseRoute 解析 "METHOD /path" 格式
//
// @ai purpose: 內部工具函式，將 TestCase.Route 字串拆分為 method 和 path
// @ai input: route string（格式 "METHOD /path"）
// @ai output: method string、path string（格式不正確時均為空字串）
// @ai sideeffect: 無
// date: 2026-05-23
func parseRoute(route string) (method, path string) {
	parts := strings.SplitN(route, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
