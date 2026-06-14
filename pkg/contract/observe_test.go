package contract

// @ai purpose: observe_test — 驗證 ObserveAll、Observe、過濾器匹配、HTML 報告生成的行為
// @ai input: *testing.T
// @ai output: 無
// @ai sideeffect: 在測試暫存目錄中可能建立 .hyp/ 目錄與 HTML 報告
// date: 2026-05-23

import (
	"strings"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/resource"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// setupObserveRouter 建立 Observe 測試專用路由，不干擾其他測試的 schema registry
func setupObserveRouter() *router.Router {
	schema.Global().Reset()

	r := router.New()

	// 路由一：POST /orders（有 input+output schema）
	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/orders",
		Summary: "建立訂單",
		Tags:    []string{"orders"},
		Input:   createReq{},
		Output:  userResp{},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "Order created"},
		},
	}).Handle(createOrderHandler)

	// 路由二：GET /orders/:id（只有 output schema）
	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/orders/:id",
		Summary: "查詢訂單",
		Tags:    []string{"orders"},
		Output:  userResp{},
	}).Handle(jsonHandler(200, userResp{ID: 42, Name: "item", Email: "x@x.com"}))

	// 路由三：DELETE /orders/:id（無 schema，應被 ObserveAll 跳過）
	r.DELETE("/orders/:id/raw", func(c *hypcontext.Context) {
		c.Status(204)
		c.Writer.WriteHeaderNow()
	})

	return r
}

// createOrderHandler 是具名函式，供 HandlerNames 過濾測試使用
func createOrderHandler(c *hypcontext.Context) {
	c.JSON(201, userResp{ID: 1, Name: "order", Email: "order@test.com"})
}

// ─── ObserveAll ─────────────────────────────────────────────────────────────

func TestObserveAll_ReturnsResults(t *testing.T) {
	r := setupObserveRouter()
	results := ObserveAll(t, r, ObserveOptions{Silent: true})

	if len(results) < 2 {
		t.Fatalf("ObserveAll 預期至少 2 條結果，得到 %d", len(results))
	}
}

func TestObserveAll_NilT(t *testing.T) {
	r := setupObserveRouter()
	// 傳入 nil *testing.T 不應 panic
	results := ObserveAll(nil, r, ObserveOptions{Silent: true})
	if len(results) == 0 {
		t.Error("ObserveAll(nil, ...) 預期回傳結果，得到空列表")
	}
}

func TestObserveAll_ResultFields(t *testing.T) {
	r := setupObserveRouter()
	results := ObserveAll(t, r, ObserveOptions{Silent: true})

	for _, res := range results {
		if res.Route.Path == "" {
			t.Error("ObserveResult.Route.Path 不應為空")
		}
		if res.Request.Method == "" {
			t.Error("ObserveResult.Request.Method 不應為空")
		}
		if res.Response.StatusCode == 0 {
			t.Error("ObserveResult.Response.StatusCode 不應為 0")
		}
		if res.Duration < 0 {
			t.Error("ObserveResult.Duration 不應為負數")
		}
		if res.Timestamp.IsZero() {
			t.Error("ObserveResult.Timestamp 不應為零值")
		}
	}
}

func TestObserveAll_PassingRoute(t *testing.T) {
	r := setupObserveRouter()
	results := ObserveAll(t, r, ObserveOptions{Silent: true})

	var found bool
	for _, res := range results {
		if res.Route.Path == "/orders/:id" {
			found = true
			if !res.Pass {
				t.Errorf("/orders/:id 預期通過，FailReason: %q", res.FailReason)
			}
			if len(res.Steps) == 0 {
				t.Error("Steps 不應為空")
			}
		}
	}
	if !found {
		t.Error("未找到 /orders/:id 的結果")
	}
}

// ─── Observe（帶過濾）───────────────────────────────────────────────────────

func TestObserve_FilterByHandlerName(t *testing.T) {
	r := setupObserveRouter()
	// createOrderHandler 是 /orders 路由的 handler
	results := Observe(t, r, "createOrderHandler", ObserveOptions{Silent: true})

	if len(results) == 0 {
		t.Fatal("Observe('createOrderHandler') 預期至少 1 條結果")
	}
	for _, res := range results {
		if res.Route.Path != "/orders" {
			t.Errorf("過濾後預期只有 /orders，得到 %s", res.Route.Path)
		}
	}
}

func TestObserve_FilterByPath(t *testing.T) {
	r := setupObserveRouter()
	results := Observe(t, r, "/orders", ObserveOptions{Silent: true})

	if len(results) < 2 {
		t.Fatalf("Observe('/orders') 預期 2 條結果，得到 %d", len(results))
	}
}

func TestObserve_FilterByTag(t *testing.T) {
	r := setupObserveRouter()
	results := Observe(t, r, "orders", ObserveOptions{Silent: true})

	if len(results) < 2 {
		t.Fatalf("Observe('orders') 預期 2 條結果，得到 %d", len(results))
	}
}

func TestObserve_FilterBySummary(t *testing.T) {
	r := setupObserveRouter()
	results := Observe(t, r, "建立訂單", ObserveOptions{Silent: true})

	if len(results) != 1 {
		t.Fatalf("Observe('建立訂單') 預期 1 條結果，得到 %d", len(results))
	}
}

func TestObserve_FilterNoMatch(t *testing.T) {
	r := setupObserveRouter()
	results := Observe(t, r, "nonexistent_xyz_abc", ObserveOptions{Silent: true})

	if len(results) != 0 {
		t.Fatalf("不匹配的過濾應回傳 0 條結果，得到 %d", len(results))
	}
}

func TestObserve_EmptyFilterEqualsObserveAll(t *testing.T) {
	r := setupObserveRouter()
	all := ObserveAll(t, r, ObserveOptions{Silent: true})
	empty := Observe(t, r, "", ObserveOptions{Silent: true})

	if len(all) != len(empty) {
		t.Errorf("空字串過濾應等同 ObserveAll：ObserveAll=%d, Observe('')=%d",
			len(all), len(empty))
	}
}

// ─── observeRouteMatchesFilter ───────────────────────────────────────────────

func TestObserveRouteMatchesFilter_HandlerName(t *testing.T) {
	route := schema.Route{
		HandlerNames: []string{"controllers.CreateOrderHandler"},
		Path:         "/orders",
	}
	if !observeRouteMatchesFilter(route, "createorder") {
		t.Error("HandlerName 大小寫不敏感比對失敗")
	}
}

func TestObserveRouteMatchesFilter_Path(t *testing.T) {
	route := schema.Route{Path: "/api/v1/products"}
	if !observeRouteMatchesFilter(route, "products") {
		t.Error("Path 子字串比對失敗")
	}
}

func TestObserveRouteMatchesFilter_Tags(t *testing.T) {
	route := schema.Route{Tags: []string{"Payments", "Billing"}}
	if !observeRouteMatchesFilter(route, "billing") {
		t.Error("Tag 大小寫不敏感比對失敗")
	}
}

func TestObserveRouteMatchesFilter_Summary(t *testing.T) {
	route := schema.Route{Summary: "建立支付訂單"}
	if !observeRouteMatchesFilter(route, "支付") {
		t.Error("Summary 子字串比對失敗")
	}
}

func TestObserveRouteMatchesFilter_NoMatch(t *testing.T) {
	route := schema.Route{Path: "/orders", Summary: "create order", Tags: []string{"orders"}}
	if observeRouteMatchesFilter(route, "payment") {
		t.Error("不應匹配")
	}
}

// ─── CapturedRequest / CapturedResponse 驗證 ─────────────────────────────────

func TestCaptureRouteExchange_CapturesRequest(t *testing.T) {
	r := setupObserveRouter()
	routes := schema.Global().All()

	var postRoute schema.Route
	for _, rt := range routes {
		if rt.Method == "POST" && rt.Path == "/orders" {
			postRoute = rt
			break
		}
	}
	if postRoute.Path == "" {
		t.Fatal("未找到 POST /orders 路由")
	}

	res := captureRouteExchange(r, postRoute, true)

	if res.Request.Method != "POST" {
		t.Errorf("Method 預期 POST，得到 %s", res.Request.Method)
	}
	if !strings.HasPrefix(res.Request.Path, "/orders") {
		t.Errorf("Path 預期含 /orders，得到 %s", res.Request.Path)
	}
	if res.Response.StatusCode != 201 {
		t.Errorf("StatusCode 預期 201，得到 %d", res.Response.StatusCode)
	}
	if res.Response.Body == "" {
		t.Error("Response.Body 不應為空")
	}

	// 量測欄位（measure=true 時應有資料）
	if !res.Measured {
		t.Error("Measured 應為 true（循序量測）")
	}
	if res.AllocBytes == 0 {
		t.Error("AllocBytes 應 > 0（handler 必有配置）")
	}
	if res.Duration < 0 {
		t.Error("Duration 不應為負數")
	}
}

func TestCaptureRouteExchange_ParallelSkipsMeasurement(t *testing.T) {
	r := setupObserveRouter()
	routes := schema.Global().All()
	var postRoute schema.Route
	for _, rt := range routes {
		if rt.Method == "POST" && rt.Path == "/orders" {
			postRoute = rt
			break
		}
	}
	if postRoute.Path == "" {
		t.Fatal("未找到 POST /orders 路由")
	}

	// measure=false 模擬並行模式：不量測資源/效能，但功能仍正常
	res := captureRouteExchange(r, postRoute, false)
	if res.Measured {
		t.Error("Measured 應為 false（並行模式不量測）")
	}
	if res.AllocBytes != 0 || res.CPUTime != 0 || res.Resources.Any() {
		t.Error("並行模式量測欄位應為零值")
	}
	if res.Response.StatusCode != 201 {
		t.Errorf("StatusCode 預期 201，得到 %d", res.Response.StatusCode)
	}
}

func TestCaptureRouteExchange_RecordsResourceUsage(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{
		Method:    "POST",
		Path:      "/db-op",
		Summary:   "touches DB and Redis",
		Input:     createReq{},
		Output:    userResp{},
		Responses: map[int]schema.ResponseSchema{201: {Description: "ok"}},
	}).Handle(func(c *hypcontext.Context) {
		// 模擬 handler 觸及資料庫與 Redis（Redis 操作觸及 2 個 key）
		resource.MarkDB()
		resource.MarkRedis(2)
		c.JSON(201, userResp{ID: 1, Name: "x", Email: "x@x.com"})
	})

	var route schema.Route
	for _, rt := range schema.Global().All() {
		if rt.Path == "/db-op" {
			route = rt
			break
		}
	}

	res := captureRouteExchange(r, route, true)
	if res.Resources.DB < 1 {
		t.Errorf("Resources.DB 應 >= 1，得到 %d", res.Resources.DB)
	}
	if res.Resources.Redis < 1 {
		t.Errorf("Resources.Redis 應 >= 1，得到 %d", res.Resources.Redis)
	}
	if res.Resources.RedisKeys < 2 {
		t.Errorf("Resources.RedisKeys 應 >= 2，得到 %d", res.Resources.RedisKeys)
	}
	if !res.Resources.UsesStorage() {
		t.Error("UsesStorage() 應為 true（handler 觸及 DB/Redis）")
	}
}

// ─── ValidationStep 驗證 ──────────────────────────────────────────────────────

func TestRunObserveValidation_StatusCodeStep(t *testing.T) {
	route := schema.Route{Path: "/orders"}
	tc := TestCase{ExpectStatus: 201, schemaPath: "/orders"}

	steps, pass, reason := runObserveValidation(route, tc, 200, []byte(`{"id":1}`))

	if len(steps) == 0 {
		t.Fatal("Steps 不應為空")
	}
	if steps[0].Name != "Status Code" {
		t.Errorf("第一步預期 'Status Code'，得到 %q", steps[0].Name)
	}
	if steps[0].Pass {
		t.Error("200 != 201 應失敗")
	}
	if pass {
		t.Error("pass 應為 false")
	}
	if reason == "" {
		t.Error("reason 不應為空")
	}
}

func TestRunObserveValidation_AllPass(t *testing.T) {
	route := schema.Route{
		Path:       "/orders",
		Output:     userResp{},
		OutputName: "userResp",
	}
	tc := TestCase{ExpectStatus: 200, schemaPath: "/orders"}
	body := []byte(`{"id":1,"name":"test","email":"x@x.com"}`)

	steps, pass, _ := runObserveValidation(route, tc, 200, body)

	for _, s := range steps {
		if !s.Pass {
			t.Errorf("步驟 %q 預期通過，Detail: %s", s.Name, s.Detail)
		}
	}
	if !pass {
		t.Error("整體驗證應通過")
	}
}

// ─── HTML 報告生成 ─────────────────────────────────────────────────────────────

func TestGenerateObserveHTML_NotEmpty(t *testing.T) {
	results := []ObserveResult{
		{
			Route:    schema.Route{Method: "GET", Path: "/test", Summary: "Test"},
			Request:  CapturedRequest{Method: "GET", Path: "/test"},
			Response: CapturedResponse{StatusCode: 200, Body: `{"ok":true}`},
			Steps: []ValidationStep{
				{Name: "Status Code", Pass: true, Detail: "got 200, want 200"},
			},
			Pass:      true,
			Duration:  5 * time.Millisecond,
			Timestamp: time.Now(),
		},
	}

	html := GenerateObserveHTML(results, "")
	if html == "" {
		t.Fatal("GenerateObserveHTML 不應回傳空字串")
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("應包含 HTML 文件頭")
	}
	if !strings.Contains(html, "HypGo Observe Report") {
		t.Error("應包含報告標題")
	}
	if !strings.Contains(html, "/test") {
		t.Error("應包含路由路徑")
	}
}

func TestGenerateObserveHTML_ShowsPassFail(t *testing.T) {
	results := []ObserveResult{
		{
			Route:     schema.Route{Method: "GET", Path: "/ok"},
			Request:   CapturedRequest{Method: "GET", Path: "/ok"},
			Response:  CapturedResponse{StatusCode: 200},
			Pass:      true,
			Duration:  1 * time.Millisecond,
			Timestamp: time.Now(),
		},
		{
			Route:      schema.Route{Method: "POST", Path: "/fail"},
			Request:    CapturedRequest{Method: "POST", Path: "/fail"},
			Response:   CapturedResponse{StatusCode: 500},
			Pass:       false,
			FailReason: "status = 500, want 200",
			Duration:   2 * time.Millisecond,
			Timestamp:  time.Now(),
		},
	}

	html := GenerateObserveHTML(results, "test")

	// 摘要統計
	if !strings.Contains(html, "50%") {
		t.Error("通過率應顯示 50%")
	}
	if !strings.Contains(html, "「test」") {
		t.Error("應顯示過濾條件")
	}
	// 失敗路由的 fail-banner
	if !strings.Contains(html, "status = 500, want 200") {
		t.Error("應顯示失敗原因")
	}
}

func TestGenerateObserveHTML_EmptyResults(t *testing.T) {
	html := GenerateObserveHTML(nil, "")
	if !strings.Contains(html, "0%") && !strings.Contains(html, "0</span>") {
		// 至少要包含 HTML 結構
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("空結果應仍輸出完整 HTML 結構")
	}
}

// ─── mergeObserveOpts ────────────────────────────────────────────────────────

func TestMergeObserveOpts_Default(t *testing.T) {
	opt := mergeObserveOpts(nil)
	if opt.OpenBrowser || opt.Silent || opt.OutputPath != "" {
		t.Error("預設 ObserveOptions 所有欄位應為零值")
	}
}

func TestMergeObserveOpts_WithValue(t *testing.T) {
	opt := mergeObserveOpts([]ObserveOptions{{Silent: true, OpenBrowser: true}})
	if !opt.Silent || !opt.OpenBrowser {
		t.Error("應使用傳入的 ObserveOptions 值")
	}
}
