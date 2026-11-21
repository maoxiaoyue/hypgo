package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// recordingServer 記下每個請求的路徑與 JSON body，一律回 reply
func recordingServer(t *testing.T, reply string) (*httptest.Server, func() []map[string]interface{}) {
	t.Helper()
	var mu sync.Mutex
	var reqs []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]interface{}{}
		json.Unmarshal(raw, &body)
		body["_path"] = r.URL.Path
		mu.Lock()
		reqs = append(reqs, body)
		mu.Unlock()
		fmt.Fprint(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]interface{}(nil), reqs...)
	}
}

func apiConfig(provider, url string) *config.LLMConfig {
	cfg := &config.LLMConfig{Mode: config.LLMModeAPI}
	cfg.API.Provider, cfg.API.BaseURL, cfg.API.Model, cfg.API.APIKey = provider, url, "m", "k"
	return cfg
}

const judgeReply = `{"choices":[{"message":{"content":"{\"score\":0.8,\"reason\":\"ok\"}"}}],` +
	`"content":[{"text":"{\"score\":0.8,\"reason\":\"ok\"}"}],"response":"{\"score\":0.8,\"reason\":\"ok\"}"}`

// LLMJudge 預設送 temperature 0；max_tokens 沒設定時不送 0。
// 歷史 bug：不送 temperature，同一份回應每次分數不同；程式內建構的設定送出 "max_tokens": 0 而被拒絕
func TestJudgeRequestIsDeterministicAndWellFormed(t *testing.T) {
	neg, warm := -1.0, 0.4
	sample := Sample{Route: "GET /x", Output: "out"}

	srv, reqs := recordingServer(t, judgeReply)
	for _, j := range []LLMJudge{
		{Rubric: "r", Config: apiConfig("openai", srv.URL)},
		{Rubric: "r", Config: apiConfig("openai", srv.URL), Temperature: &warm},
		{Rubric: "r", Config: apiConfig("openai", srv.URL), Temperature: &neg},
		{Rubric: "r", Config: apiConfig("anthropic", srv.URL)},
	} {
		if res, err := j.Grade(context.Background(), sample); err != nil || res.Score != 0.8 {
			t.Fatalf("grade = %+v, %v", res, err)
		}
	}
	got := reqs()
	if got[0]["temperature"] != 0.0 || got[1]["temperature"] != 0.4 {
		t.Errorf("temperature = %v / %v, want 0 / 0.4", got[0]["temperature"], got[1]["temperature"])
	}
	if _, sent := got[2]["temperature"]; sent {
		t.Error("a negative Temperature should omit the parameter")
	}
	if _, sent := got[0]["max_tokens"]; sent {
		t.Errorf("max_tokens = %v sent although it is not configured", got[0]["max_tokens"])
	}
	if got[3]["max_tokens"] != float64(defaultMaxTokens) || got[3]["temperature"] != 0.0 {
		t.Errorf("anthropic request = %v", got[3])
	}

	// Ollama：temperature 放在 options
	ollama, oreqs := recordingServer(t, judgeReply)
	cfg := &config.LLMConfig{Mode: config.LLMModeOllama}
	cfg.Ollama.URL, cfg.Ollama.Model = ollama.URL, "m"
	if _, err := (LLMJudge{Rubric: "r", Config: cfg}).Grade(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if opts, _ := oreqs()[0]["options"].(map[string]interface{}); opts["temperature"] != 0.0 {
		t.Errorf("ollama options = %v", oreqs()[0]["options"])
	}
}

// 空的回應直接 0 分、不呼叫 API；Golden 的 embedding 只取一次。
// 歷史 bug：空的回應送出 {"input": ""} 而得到 API 錯誤；每次評分都重新對 Golden 取 embedding
func TestSimilarityEmptyOutputAndGoldenCache(t *testing.T) {
	srv, reqs := recordingServer(t, `{"data":[{"embedding":[1,0,0]}]}`)
	g := SimilarityGrader{Golden: "golden " + t.Name(), Config: apiConfig("openai", srv.URL)}

	res, err := g.Grade(context.Background(), Sample{Output: "  \n"})
	if err != nil || res.Score != 0 || res.Reason == "" || len(reqs()) != 0 {
		t.Fatalf("empty output: %+v, %v, %d request(s)", res, err, len(reqs()))
	}

	for i := 0; i < 3; i++ {
		if res, err := g.Grade(context.Background(), Sample{Output: "out"}); err != nil || res.Score != 1 {
			t.Fatalf("grade = %+v, %v", res, err)
		}
	}
	golden := 0
	for _, r := range reqs() {
		if r["input"] == g.Golden {
			golden++
		}
	}
	if golden != 1 || len(reqs()) != 4 {
		t.Errorf("golden embedded %d time(s) in %d requests, want 1 in 4", golden, len(reqs()))
	}
}

// 歷史紀錄：fail_reason 有長度上限；case 與 grader_ms 寫得進去、讀得回來。
// 歷史 bug：失敗原因引用整份回應，原樣寫進歷史檔
func TestHistoryRecordFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.jsonl")
	long := strings.Repeat("回應內容", 200)
	if err := AppendHistoryTo(path, EvalRecord{Route: "GET /x", Case: "happy", Status: "fail", FailReason: long, LatencyMs: 2, GraderMs: 900}); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadHistory(path)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %v, %v", recs, err)
	}
	r := recs[0]
	if len(r.FailReason) > MaxFailReason+len("…") || !strings.HasSuffix(r.FailReason, "…") || !strings.HasPrefix(long, strings.TrimSuffix(r.FailReason, "…")) {
		t.Errorf("fail_reason = %d bytes %q", len(r.FailReason), r.FailReason)
	}
	if r.Case != "happy" || r.GraderMs != 900 || r.LatencyMs != 2 {
		t.Errorf("record = %+v", r)
	}
}
