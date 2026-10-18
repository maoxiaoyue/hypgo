package manifest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// countingOllama 計算請求次數（批次回覆 id 1–3）；fail 為 true 時回 500
func countingOllama(t *testing.T, fail *atomic.Bool) (*config.LLMConfig, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail != nil && fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"response":"[{\"id\":1,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"},{\"id\":2,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"},{\"id\":3,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"}]"}`))
	}))
	t.Cleanup(srv.Close)
	return &config.LLMConfig{Mode: config.LLMModeOllama, Ollama: config.OllamaConfig{URL: srv.URL, Model: "m1"}}, &calls
}

// TestLLMCacheSkipsUnchangedRoutes 迴歸測試：路由沒變就沿用上次 LLM 結果，不再請求。
// 先前每次重新生成 manifest 都對每條路由重打一次 LLM。
func TestLLMCacheSkipsUnchangedRoutes(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.GET("/api/a", listOrdersHandler)
	r.GET("/api/b", listOrdersHandler)

	dir := t.TempDir()
	path := filepath.Join(dir, "context.yaml")
	cachePath := filepath.Join(dir, DefaultLLMCacheFilename)
	llmCfg, calls := countingOllama(t, nil)
	sync := func(cfg *config.LLMConfig) {
		t.Helper()
		if err := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, LLMConfig: cfg}, r, nil, nil).Sync(); err != nil {
			t.Fatal(err)
		}
	}

	sync(llmCfg) // 兩條路由合併成一個請求
	if got := calls.Load(); got != 1 {
		t.Fatalf("first sync made %d LLM requests, want 1", got)
	}

	sync(llmCfg) // 全部命中
	if got := calls.Load(); got != 1 {
		t.Errorf("second sync made %d extra LLM requests, want 0", got-1)
	}
	if b, _ := os.ReadFile(path); strings.Count(string(b), "LLM SUMMARY") != 2 {
		t.Errorf("cached results not applied to manifest:\n%s", b)
	}

	// 新增一條：只請求新路由
	r.GET("/api/c", listOrdersHandler)
	sync(llmCfg)
	if got := calls.Load(); got != 2 {
		t.Errorf("after adding a route: %d total requests, want 2", got)
	}

	// 換模型：鍵改變，全部重新請求
	other := *llmCfg
	other.Ollama.Model = "m2"
	sync(&other)
	if got := calls.Load(); got != 3 {
		t.Errorf("after switching model: %d total requests, want 3", got)
	}

	// 快取只留本次用到的條目（m1 的舊條目被淘汰）
	if n := len(loadLLMCache(cachePath)); n != 3 {
		t.Errorf("cache has %d entries, want 3 (stale entries pruned)", n)
	}
}

// TestLLMCacheDoesNotStoreFailures 迴歸測試：LLM 失敗的路由不寫入快取，下次照樣重試。
func TestLLMCacheDoesNotStoreFailures(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.GET("/api/a", listOrdersHandler)

	dir := t.TempDir()
	path := filepath.Join(dir, "context.yaml")
	var fail atomic.Bool
	fail.Store(true)
	llmCfg, calls := countingOllama(t, &fail)
	sync := func() {
		t.Helper()
		if err := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, LLMConfig: llmCfg}, r, nil, nil).Sync(); err != nil {
			t.Fatal(err)
		}
	}

	sync() // 失敗：退回推斷，不快取
	fail.Store(false)
	sync() // 必須重試
	if got := calls.Load(); got != 2 {
		t.Errorf("LLM requests = %d, want 2 (failed route retried)", got)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "LLM SUMMARY") {
		t.Errorf("retry result not applied:\n%s", b)
	}
}
