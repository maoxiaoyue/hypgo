package contract

// @ai purpose: engine_test — 驗證 RunConfig 執行引擎的並行、重試、速率限制、FailFast、RecordHistory 與向後相容性
// @ai input: *testing.T
// @ai output: 無
// @ai sideeffect: 在測試暫存目錄中可能建立 .hyp/ 目錄與 HTML 報告；RecordHistory 測試寫入 eval_history.jsonl
// date: 2026-05-23

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/eval"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// ─── mergeRunConfig ──────────────────────────────────────────────────────────

func TestMergeRunConfig_Defaults(t *testing.T) {
	cfg := mergeRunConfig(nil)
	if cfg.MaxWorkers != runtime.GOMAXPROCS(0) {
		t.Errorf("MaxWorkers default = %d, want GOMAXPROCS = %d",
			cfg.MaxWorkers, runtime.GOMAXPROCS(0))
	}
	if cfg.Parallel {
		t.Error("Parallel should default to false")
	}
	if cfg.FailFast {
		t.Error("FailFast should default to false")
	}
	if cfg.RetryCount != 0 {
		t.Errorf("RetryCount default = %d, want 0", cfg.RetryCount)
	}
	if cfg.RateLimit != 0 {
		t.Errorf("RateLimit default = %d, want 0", cfg.RateLimit)
	}
}

func TestMergeRunConfig_ExplicitValues(t *testing.T) {
	cfg := mergeRunConfig([]RunConfig{{
		Parallel:   true,
		MaxWorkers: 2,
		RetryCount: 3,
		RetryDelay: 100 * time.Millisecond,
		RateLimit:  5,
		FailFast:   true,
	}})
	if !cfg.Parallel {
		t.Error("Parallel should be true")
	}
	if cfg.MaxWorkers != 2 {
		t.Errorf("MaxWorkers = %d, want 2", cfg.MaxWorkers)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", cfg.RetryCount)
	}
	if cfg.RetryDelay != 100*time.Millisecond {
		t.Errorf("RetryDelay = %v, want 100ms", cfg.RetryDelay)
	}
	if cfg.RateLimit != 5 {
		t.Errorf("RateLimit = %d, want 5", cfg.RateLimit)
	}
	if !cfg.FailFast {
		t.Error("FailFast should be true")
	}
}

func TestMergeRunConfig_ZeroMaxWorkers_GetsGOMAXPROCS(t *testing.T) {
	cfg := mergeRunConfig([]RunConfig{{Parallel: true, MaxWorkers: 0}})
	if cfg.MaxWorkers != runtime.GOMAXPROCS(0) {
		t.Errorf("zero MaxWorkers should become GOMAXPROCS=%d, got %d",
			runtime.GOMAXPROCS(0), cfg.MaxWorkers)
	}
}

// ─── runTestOnce（純函式驗證）────────────────────────────────────────────────

func TestRunTestOnce_PassOnValidRoute(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.GET("/ping", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"status": "ok"})
	})

	pass, reason := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 200,
	})
	if !pass {
		t.Errorf("runTestOnce should pass, reason: %s", reason)
	}
	if reason != "" {
		t.Errorf("reason should be empty on success, got %q", reason)
	}
}

func TestRunTestOnce_FailOnWrongStatus(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.GET("/ping", func(c *hypcontext.Context) {
		c.JSON(200, nil)
	})

	pass, reason := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 500, // 故意錯誤
	})
	if pass {
		t.Error("runTestOnce should fail when status does not match")
	}
	if reason == "" {
		t.Error("reason should not be empty on failure")
	}
}

func TestRunTestOnce_InvalidRouteFormat(t *testing.T) {
	r := router.New()
	pass, reason := runTestOnce(r, TestCase{Route: "INVALID"})
	if pass {
		t.Error("invalid route format should fail")
	}
	if reason == "" {
		t.Error("reason should describe the invalid format")
	}
}

// ─── TestAll 向後相容 ─────────────────────────────────────────────────────────

func TestTestAll_BackwardCompat(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/compat",
		Summary: "backward compat test",
	}).Handle(func(c *hypcontext.Context) {
		c.JSON(200, nil)
	})

	// TestAll(t, r) 不帶 RunConfig 應正常執行
	TestAll(t, r)
}

// ─── 重試機制 ─────────────────────────────────────────────────────────────────

func TestTestAll_RetrySucceeds(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	var callCount atomic.Int32

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/flaky",
		Summary: "flaky route",
	}).Handle(func(c *hypcontext.Context) {
		n := callCount.Add(1)
		if n < 3 {
			// 前 2 次回傳 500
			c.JSON(500, map[string]string{"error": "not ready"})
			return
		}
		c.JSON(200, nil)
	})

	// RetryCount=3：最多執行 4 次（1 次初始 + 3 次重試）
	// handler 第 3 次才回 200，在重試窗口內，應整體通過
	TestAll(t, r, RunConfig{RetryCount: 3, RetryDelay: 0})

	got := callCount.Load()
	if got < 3 {
		t.Errorf("handler called %d times, expected at least 3", got)
	}
}

func TestObserve_RetrySucceeds(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	var callCount atomic.Int32

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/flaky2",
		Summary: "flaky observe route",
	}).Handle(func(c *hypcontext.Context) {
		n := callCount.Add(1)
		if n < 2 {
			c.JSON(500, nil)
			return
		}
		c.JSON(200, nil)
	})

	results := ObserveAll(t, r, ObserveOptions{
		Silent: true,
		Run:    RunConfig{RetryCount: 3, RetryDelay: 0},
	})

	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if !results[0].Pass {
		t.Errorf("result should pass after retry, FailReason: %s", results[0].FailReason)
	}
	if callCount.Load() < 2 {
		t.Errorf("handler called %d times, expected at least 2", callCount.Load())
	}
}

// ─── FailFast ─────────────────────────────────────────────────────────────────

func TestObserve_FailFast_StopsEarly(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	var executedCount atomic.Int32

	makeHandler := func(status int) hypcontext.HandlerFunc {
		return func(c *hypcontext.Context) {
			executedCount.Add(1)
			c.JSON(status, nil)
		}
	}

	// 路由 A：回傳 500（schema 期望 200），會失敗
	r.Schema(schema.Route{Method: "GET", Path: "/a-fails", Summary: "fails"}).
		Handle(makeHandler(500))
	// 路由 B、C：正常（但 FailFast 後不應被執行）
	r.Schema(schema.Route{Method: "GET", Path: "/b-ok", Summary: "ok"}).
		Handle(makeHandler(200))
	r.Schema(schema.Route{Method: "GET", Path: "/c-ok", Summary: "ok2"}).
		Handle(makeHandler(200))

	results := ObserveAll(t, r, ObserveOptions{
		Silent: true,
		Run:    RunConfig{FailFast: true},
	})

	// FailFast 應在第一個失敗後停止，results 數量應少於 3
	if len(results) >= 3 {
		t.Errorf("FailFast should stop early, got %d results (want < 3)", len(results))
	}
	// 至少有一個失敗結果
	hasFailure := false
	for _, r := range results {
		if !r.Pass {
			hasFailure = true
			break
		}
	}
	if !hasFailure {
		t.Error("expected at least one failure result when FailFast triggered")
	}
}

// ─── 速率限制 ─────────────────────────────────────────────────────────────────

func TestObserve_RateLimit_SlowsExecution(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	for _, p := range []string{"/r1", "/r2", "/r3"} {
		path := p
		r.Schema(schema.Route{Method: "GET", Path: path, Summary: path}).
			Handle(func(c *hypcontext.Context) { c.JSON(200, nil) })
	}

	// RateLimit=2 tests/sec：3 條路由之間有 2 次 sleep(500ms)
	// 循序模式下執行：第 1 條前無 sleep，第 2 條前 sleep 500ms，第 3 條前 sleep 500ms
	// 總耗時應 ≥ ~1s
	start := time.Now()
	ObserveAll(t, r, ObserveOptions{
		Silent: true,
		Run:    RunConfig{RateLimit: 2},
	})
	elapsed := time.Since(start)

	// 3 條路由、RateLimit=2：應有 2 次 500ms 的速率限制等待 → ≥ ~900ms
	if elapsed < 900*time.Millisecond {
		t.Errorf("rate limited run took %v, expected >= 900ms for 3 routes at 2/s", elapsed)
	}
}

// ─── 並行加速 ────────────────────────────────────────────────────────────────

func TestObserve_Parallel_FasterThanSequential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive test in short mode")
	}

	schema.Global().Reset()
	r := router.New()

	// 4 條路由，每條 handler 各 sleep 60ms
	for _, p := range []string{"/p1", "/p2", "/p3", "/p4"} {
		path := p
		r.Schema(schema.Route{Method: "GET", Path: path, Summary: path}).
			Handle(func(c *hypcontext.Context) {
				time.Sleep(60 * time.Millisecond)
				c.JSON(200, nil)
			})
	}

	// 循序：預期 ~240ms
	start := time.Now()
	ObserveAll(t, r, ObserveOptions{Silent: true})
	seqDur := time.Since(start)

	// 並行（4 workers）：預期 ~60ms
	start = time.Now()
	ObserveAll(t, r, ObserveOptions{
		Silent: true,
		Run:    RunConfig{Parallel: true, MaxWorkers: 4},
	})
	parDur := time.Since(start)

	// 並行應比循序快至少 40%
	threshold := time.Duration(float64(seqDur) * 0.6)
	if parDur >= threshold {
		t.Errorf("parallel (%v) should be faster than 60%% of sequential (%v = threshold %v)",
			parDur, seqDur, threshold)
	}
}

// ─── 並行結果完整性 ───────────────────────────────────────────────────────────

func TestObserve_Parallel_AllResultsCollected(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	for _, p := range []string{"/pa", "/pb", "/pc"} {
		path := p
		r.Schema(schema.Route{Method: "GET", Path: path, Summary: path}).
			Handle(func(c *hypcontext.Context) { c.JSON(200, nil) })
	}

	results := ObserveAll(t, r, ObserveOptions{
		Silent: true,
		Run:    RunConfig{Parallel: true, MaxWorkers: 3},
	})

	if len(results) != 3 {
		t.Errorf("parallel mode should collect all 3 results, got %d", len(results))
	}
	for _, res := range results {
		if res.Route.Path == "" {
			t.Error("result should have non-empty Route.Path")
		}
		if res.Response.StatusCode == 0 {
			t.Error("result should have non-zero StatusCode")
		}
	}
}

// ─── TestAll + 並行 ────────────────────────────────────────────────────────────

func TestTestAll_Parallel(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	for _, p := range []string{"/q1", "/q2", "/q3", "/q4"} {
		path := p
		r.Schema(schema.Route{Method: "GET", Path: path, Summary: path}).
			Handle(func(c *hypcontext.Context) { c.JSON(200, nil) })
	}

	// 驗證並行模式不會 panic 且所有子測試通過
	TestAll(t, r, RunConfig{Parallel: true, MaxWorkers: 4})
}

// ─── ObserveOptions.Run 零值向後相容 ──────────────────────────────────────────

func TestObserve_ZeroRunConfig_BackwardCompat(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/compat2", Summary: "compat"}).
		Handle(func(c *hypcontext.Context) { c.JSON(200, nil) })

	// ObserveOptions 不設定 Run 欄位 → 使用零值 RunConfig → 原有行為
	results := ObserveAll(t, r, ObserveOptions{Silent: true})
	if len(results) == 0 {
		t.Error("expected results even with zero RunConfig")
	}
}

// ─── RecordHistory ────────────────────────────────────────────────────────────

func TestTestAll_RecordHistory_WritesJSONL(t *testing.T) {
	schema.Global().Reset()
	r := router.New()

	r.Schema(schema.Route{Method: "GET", Path: "/hist1", Summary: "history test 1"}).
		Handle(func(c *hypcontext.Context) { c.JSON(200, nil) })
	r.Schema(schema.Route{Method: "POST", Path: "/hist2", Summary: "history test 2"}).
		Handle(func(c *hypcontext.Context) { c.JSON(201, nil) })

	// 使用臨時目錄，切換工作目錄使 .hyp/ 寫入到 tmpDir
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// RecordHistory=true：應在 .hyp/eval_history.jsonl 中寫入 2 筆記錄
	TestAll(t, r, RunConfig{RecordHistory: true})

	histPath := filepath.Join(tmpDir, eval.DefaultHistoryPath())
	records, loadErr := eval.LoadHistory(histPath)
	if loadErr != nil {
		t.Fatalf("LoadHistory failed: %v", loadErr)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 history records, got %d", len(records))
	}
	for _, rec := range records {
		if rec.Status == "" {
			t.Error("record.Status should not be empty")
		}
		if rec.LatencyMs < 0 {
			t.Errorf("record.LatencyMs = %d, want >= 0", rec.LatencyMs)
		}
		if rec.Scores["contract"] != 1.0 {
			t.Errorf("record.Scores[contract] = %f, want 1.0 for passing route", rec.Scores["contract"])
		}
	}
}

func TestTestAll_RecordHistory_FailAndPassRecords(t *testing.T) {
	// 直接驗證 eval.AppendHistoryTo 可同時正確記錄 status=fail/pass 與對應分數
	// 不透過 TestAll（避免失敗子測試汙染 t），在 eval 層面驗證記錄格式往返
	tmpDir := t.TempDir()
	histPath := filepath.Join(tmpDir, "eval_roundtrip.jsonl")

	records := []eval.EvalRecord{
		{
			Timestamp:  time.Now().UTC(),
			Route:      "GET /hist-fail",
			Status:     "fail",
			Scores:     map[string]float64{"contract": 0.0},
			LatencyMs:  1,
			FailReason: "[GET /hist-fail] status = 500, want 200",
		},
		{
			Timestamp: time.Now().UTC(),
			Route:     "GET /hist-pass",
			Status:    "pass",
			Scores:    map[string]float64{"contract": 1.0},
			LatencyMs: 5,
		},
	}

	for _, rec := range records {
		if err := eval.AppendHistoryTo(histPath, rec); err != nil {
			t.Fatalf("AppendHistoryTo: %v", err)
		}
	}

	loaded, err := eval.LoadHistory(histPath)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 records, got %d", len(loaded))
	}
	if loaded[0].Status != "fail" || loaded[0].Scores["contract"] != 0.0 {
		t.Errorf("loaded[0]: status=%q scores=%v, want status=fail contract=0.0",
			loaded[0].Status, loaded[0].Scores)
	}
	if loaded[0].FailReason == "" {
		t.Error("loaded[0].FailReason should not be empty for a failing record")
	}
	if loaded[1].Status != "pass" || loaded[1].Scores["contract"] != 1.0 {
		t.Errorf("loaded[1]: status=%q scores=%v, want status=pass contract=1.0",
			loaded[1].Status, loaded[1].Scores)
	}
}

func TestMergeRunConfig_RecordHistory_DefaultFalse(t *testing.T) {
	cfg := mergeRunConfig(nil)
	if cfg.RecordHistory {
		t.Error("RecordHistory should default to false")
	}
}

func TestMergeRunConfig_RecordHistory_Explicit(t *testing.T) {
	cfg := mergeRunConfig([]RunConfig{{RecordHistory: true}})
	if !cfg.RecordHistory {
		t.Error("RecordHistory should be true when explicitly set")
	}
}
