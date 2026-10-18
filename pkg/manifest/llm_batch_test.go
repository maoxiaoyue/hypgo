package manifest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// batchOllama 依 prompt 裡的每條路由回覆 "S <path>" / "D <path>"（id 對應 prompt 中的編號）；
// onRequest 在回覆前被呼叫，n 為第幾個請求
func batchOllama(t *testing.T, onRequest func(n int32)) (*config.LLMConfig, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if onRequest != nil {
			onRequest(n)
		}
		var req struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		items := []map[string]any{}
		for i, block := range strings.Split(req.Prompt, "\nRoute ")[1:] {
			path := ""
			if _, rest, ok := strings.Cut(block, "Path: "); ok {
				path, _, _ = strings.Cut(rest, "\n")
			}
			items = append(items, map[string]any{"id": i + 1, "summary": "S " + path, "description": "D " + path})
		}
		reply, _ := json.Marshal(items)
		json.NewEncoder(w).Encode(map[string]string{"response": string(reply)})
	}))
	t.Cleanup(srv.Close)
	return &config.LLMConfig{Mode: config.LLMModeOllama, Ollama: config.OllamaConfig{URL: srv.URL, Model: "m"}}, &calls
}

func routerWithRoutes(n int) *router.Router {
	r := router.New()
	for i := 0; i < n; i++ {
		r.GET(fmt.Sprintf("/r/%02d", i), listOrdersHandler)
	}
	return r
}

// TestLLMBatchesRoutes 迴歸測試：多條路由合併成一個 LLM 請求（每批 llmBatchSize 條），結果依 id 回填到正確的路由。
// 先前每條路由各送一次請求，指令與往返延遲重複 N 次。
func TestLLMBatchesRoutes(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	llmCfg, calls := batchOllama(t, nil)
	enr, err := NewLLMEnricherFromConfig(llmCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultEnrichConfig()
	cfg.LLMEnricher = enr

	m := NewCollectorWithEnrich(routerWithRoutes(25), nil, cfg).Collect()
	if got := calls.Load(); got != 2 {
		t.Errorf("25 routes made %d LLM requests, want 2", got)
	}
	for _, rm := range m.Routes {
		if rm.Summary != "S "+rm.Path || rm.Description != "D "+rm.Path {
			t.Errorf("%s: summary=%q description=%q, want results for its own path", rm.Path, rm.Summary, rm.Description)
		}
	}
}

// TestLLMSkipsDocumentedRoutes 迴歸測試：Summary 與 Description 都已宣告的路由不送 LLM。
// 先前照樣送出；LLM 只回填空白欄位，結果為空、不進快取，每次同步都重送一次。
func TestLLMSkipsDocumentedRoutes(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/doc", Summary: "Declared", Description: "Declared desc"}).Handle(listOrdersHandler)
	r.GET("/undoc", listOrdersHandler)

	llmCfg, calls := batchOllama(t, nil)
	path := filepath.Join(t.TempDir(), "context.yaml")
	for i := 0; i < 2; i++ {
		if err := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, LLMConfig: llmCfg}, r, nil, nil).Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("LLM requests over two syncs = %d, want 1 (only /undoc, once)", got)
	}
}

// TestLLMCacheSavedPerBatch 迴歸測試：每批完成就寫入快取，同步途中被中斷（server 停止、hyp run 熱重載）
// 也保得住已完成的批次。先前只在全部路由跑完後存一次，中斷即全部重來。
func TestLLMCacheSavedPerBatch(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, DefaultLLMCacheFilename)

	var savedBeforeSecond atomic.Int32
	llmCfg, _ := batchOllama(t, func(n int32) {
		if n == 2 {
			savedBeforeSecond.Store(int32(len(loadLLMCache(cachePath))))
		}
	})
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: filepath.Join(dir, "context.yaml"), LLMConfig: llmCfg}, routerWithRoutes(25), nil, nil)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := savedBeforeSecond.Load(); got != llmBatchSize {
		t.Errorf("cache entries on disk when the second request started = %d, want %d", got, llmBatchSize)
	}
	if got := len(loadLLMCache(cachePath)); got != 25 {
		t.Errorf("final cache entries = %d, want 25", got)
	}
}
