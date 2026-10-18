package manifest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type regressIn struct {
	Name string `json:"name"`
}

func regressAuth(c *hypcontext.Context)       { c.Next() }
func listOrdersHandler(c *hypcontext.Context) {}

// fakeOllama 回傳固定 summary / description 的 Ollama 伺服器（批次回覆 id 1–3）；delay 模擬慢速 LLM
func fakeOllama(t *testing.T, delay time.Duration) *config.LLMConfig {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Write([]byte(`{"response":"[{\"id\":1,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"},{\"id\":2,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"},{\"id\":3,\"summary\":\"LLM SUMMARY\",\"description\":\"LLM DESC\"}]"}`))
	}))
	t.Cleanup(srv.Close)
	return &config.LLMConfig{Mode: config.LLMModeOllama, Ollama: config.OllamaConfig{URL: srv.URL, Model: "m"}}
}

func routeByPath(m *Manifest, path string) *RouteManifest {
	for i := range m.Routes {
		if m.Routes[i].Path == path {
			return &m.Routes[i]
		}
	}
	return nil
}

// TestDefaultManifestIncludesFields 回歸測試（A1）：預設即輸出 Input / Output 的欄位形狀。
// 歷史 bug：IncludeFields 預設 false 且無呼叫端打開，manifest 從不含欄位資訊。
func TestDefaultManifestIncludesFields(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "POST", Path: "/f", Input: regressIn{}}).Handle(listOrdersHandler)

	m := NewCollector(r, nil).Collect()
	if rm := routeByPath(m, "/f"); rm == nil || rm.InputType != "regressIn" {
		t.Fatalf("route = %+v, want input_type regressIn", rm)
	}
	if f := m.Types["regressIn"]; len(f) != 1 || f[0].Name != "name" {
		t.Errorf("types[regressIn] = %+v, want [name]", f)
	}
}

// TestLLMSummaryIsUsed 回歸測試（A2）：LLM 的 summary 會被採用，但不覆蓋人寫的 Summary；LLM 失敗時退回推斷。
// 歷史 bug：推斷先填滿 Summary，LLM 只回填空白欄位，產生的 summary 永遠被丟棄。
func TestLLMSummaryIsUsed(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/api/orders"}).Handle(listOrdersHandler)
	r.Schema(schema.Route{Method: "GET", Path: "/api/declared", Summary: "Human summary"}).Handle(listOrdersHandler)

	enr, err := NewLLMEnricherFromConfig(fakeOllama(t, 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultEnrichConfig()
	cfg.LLMEnricher = enr
	m := NewCollectorWithEnrich(r, nil, cfg).Collect()
	if got := routeByPath(m, "/api/orders"); got.Summary != "LLM SUMMARY" || got.Description != "LLM DESC" {
		t.Errorf("LLM result not applied: summary=%q description=%q", got.Summary, got.Description)
	}
	if got := routeByPath(m, "/api/declared"); got.Summary != "Human summary" {
		t.Errorf("declared summary overwritten: %q", got.Summary)
	}

	broken, _ := NewLLMEnricherFromConfig(&config.LLMConfig{Mode: config.LLMModeOllama, Ollama: config.OllamaConfig{URL: "http://127.0.0.1:1", Model: "m", Timeout: 1}}, nil)
	cfg.LLMEnricher = broken
	if got := routeByPath(NewCollectorWithEnrich(r, nil, cfg).Collect(), "/api/orders"); got.Summary == "" {
		t.Error("summary inference must still run when the LLM fails")
	}
}

// TestAutoSyncSkipsUnchangedContent 回歸測試（A3）：內容沒變就不重寫，generated_at 維持原值。
// 歷史 bug：每次啟動都只因 generated_at 改動檔案，納入版控的 context.yaml 永遠是 modified。
func TestAutoSyncSkipsUnchangedContent(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.GET("/a", listOrdersHandler)
	path := filepath.Join(t.TempDir(), "context.yaml")
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, DisableLLMAutoDiscovery: true}, r, nil, nil)

	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	time.Sleep(10 * time.Millisecond)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); string(second) != string(first) {
		t.Error("unchanged manifest was rewritten")
	}

	r.GET("/b", listOrdersHandler) // 內容真的變了 → 要重寫
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if third, _ := os.ReadFile(path); !strings.Contains(string(third), "/b") {
		t.Error("changed manifest was not written")
	}
}

// TestAutoSyncRunDoesNotBlockOnLLM 回歸測試（A4）：設定了 LLM 時 Run 於背景執行，不卡住啟動。
// 歷史 bug：server.Start 同步呼叫 SyncSafe，每條路由一次 LLM 請求，啟動被卡住。
func TestAutoSyncRunDoesNotBlockOnLLM(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.GET("/slow", listOrdersHandler)
	path := filepath.Join(t.TempDir(), "context.yaml")
	a := NewAutoSync(AutoSyncConfig{Enabled: true, Path: path, LLMConfig: fakeOllama(t, 300*time.Millisecond)}, r, nil, nil)

	start := time.Now()
	a.Run()
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("Run blocked for %v with LLM configured", d)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "LLM SUMMARY") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background sync did not write the LLM-enriched manifest")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSummaryInferenceQuality 回歸測試（A5–A7）。
func TestSummaryInferenceQuality(t *testing.T) {
	schema.Global().Reset()
	defer schema.Global().Reset()
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/api/orders"}).Handle(regressAuth, listOrdersHandler)
	r.Schema(schema.Route{Method: "GET", Path: "/api/items"}).Handle(func(c *hypcontext.Context) {})
	m := NewCollector(r, nil).Collect()

	// A5：取最後一個具名 handler（不是中介層）；匿名 handler 改用路徑推斷
	if got := routeByPath(m, "/api/orders").Summary; got != "List Orders" {
		t.Errorf("summary with middleware = %q, want List Orders", got)
	}
	if got := routeByPath(m, "/api/items").Summary; got != "List Items" {
		t.Errorf("summary for anonymous handler = %q, want List Items", got)
	}

	// A6：連續大寫視為縮寫
	for in, want := range map[string]string{"GetByID": "Get By ID", "CreateUser": "Create User", "HTTPServer": "HTTP Server", "listV2Items": "list V2 Items"} {
		if got := humanizeMethod(in); got != want {
			t.Errorf("humanizeMethod(%q) = %q, want %q", in, got, want)
		}
	}

	// A7：非 ASCII 不被切壞
	if got := capitalize("訂單"); got != "訂單" {
		t.Errorf("capitalize(訂單) = %q", got)
	}
	if got := humanize("query_訂單"); got != "Query 訂單" {
		t.Errorf("humanize = %q", got)
	}
	if got := sanitizeResponse(strings.Repeat("中", 200)); !utf8.ValidString(got) || len(got) > 500 {
		t.Errorf("sanitizeResponse produced invalid UTF-8 or too long (%d bytes)", len(got))
	}
}
