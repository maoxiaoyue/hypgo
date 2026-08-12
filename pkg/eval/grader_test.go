package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// --- SchemaGrader ---

func TestSchemaGrader(t *testing.T) {
	pass := SchemaGrader{Validate: func(string) error { return nil }}
	res, err := pass.Grade(context.Background(), Sample{Output: "{}"})
	if err != nil || res.Score != 1.0 {
		t.Errorf("pass case: score = %v, err = %v; want 1.0, nil", res.Score, err)
	}

	fail := SchemaGrader{Validate: func(string) error { return fmt.Errorf("missing field") }}
	res, err = fail.Grade(context.Background(), Sample{Output: "{}"})
	if err != nil || res.Score != 0.0 || res.Reason != "missing field" {
		t.Errorf("fail case: got %+v, err %v; want score 0 with reason", res, err)
	}

	if _, err := (SchemaGrader{}).Grade(context.Background(), Sample{}); err == nil {
		t.Error("nil Validate should error")
	}
}

// --- Aggregate ---

func TestAggregate(t *testing.T) {
	results := []GraderResult{
		{Name: "a", Score: 1.0},
		{Name: "b", Score: 0.5},
	}
	if got := Aggregate(results, nil); math.Abs(got-0.75) > 1e-9 {
		t.Errorf("equal weights: got %v, want 0.75", got)
	}
	// a 權重 3、b 權重 1 → (3.0+0.5)/4 = 0.875
	if got := Aggregate(results, map[string]float64{"a": 3}); math.Abs(got-0.875) > 1e-9 {
		t.Errorf("weighted: got %v, want 0.875", got)
	}
	if got := Aggregate(nil, nil); got != 0 {
		t.Errorf("empty: got %v, want 0", got)
	}
	// 權重 0 的評判器不計入
	if got := Aggregate(results, map[string]float64{"b": 0}); got != 1.0 {
		t.Errorf("zero weight excluded: got %v, want 1.0", got)
	}
}

// --- cosineSimilarity ---

func TestCosineSimilarity(t *testing.T) {
	if got, _ := cosineSimilarity([]float64{1, 0}, []float64{1, 0}); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical vectors: got %v, want 1", got)
	}
	if got, _ := cosineSimilarity([]float64{1, 0}, []float64{0, 1}); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal vectors: got %v, want 0", got)
	}
	if _, err := cosineSimilarity([]float64{1}, []float64{1, 2}); err == nil {
		t.Error("dimension mismatch should error")
	}
	if _, err := cosineSimilarity([]float64{0, 0}, []float64{1, 0}); err == nil {
		t.Error("zero vector should error")
	}
}

// --- LLMJudge（ollama wire format）---

func TestLLMJudgeOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// Ollama 把模型輸出放在 response 欄位（字串）
		json.NewEncoder(w).Encode(map[string]string{
			"response": `{"score": 0.85, "reason": "covers all key points"}`,
		})
	}))
	defer srv.Close()

	judge := LLMJudge{
		Rubric: "摘要是否涵蓋重點？",
		Config: &config.LLMConfig{
			Mode:   config.LLMModeOllama,
			Ollama: config.OllamaConfig{URL: srv.URL, Model: "test-model", Timeout: 5},
		},
	}
	res, err := judge.Grade(context.Background(), Sample{Route: "POST /api/summarize", Output: `{"summary":"..."}`})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if math.Abs(res.Score-0.85) > 1e-9 || res.Reason != "covers all key points" {
		t.Errorf("got %+v", res)
	}
	if res.Name != "llm_judge" {
		t.Errorf("Name = %q", res.Name)
	}
}

// --- LLMJudge（openai 相容 wire format + 分數夾擠 + 容錯解析）---

func TestLLMJudgeOpenAICompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		// 模型在 JSON 前後多話 → parseVerdict 應容錯；score 超界 → 應夾到 1.0
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "Sure! {\"score\": 1.7, \"reason\": \"ok\"} hope that helps"}},
			},
		})
	}))
	defer srv.Close()

	judge := LLMJudge{
		Rubric: "rubric",
		Config: &config.LLMConfig{
			Mode: config.LLMModeAPI,
			API:  config.APIConfig{Provider: "custom", BaseURL: srv.URL, Model: "m", APIKey: "test-key", Timeout: 5},
		},
	}
	res, err := judge.Grade(context.Background(), Sample{Output: "x"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Score != 1.0 {
		t.Errorf("out-of-range score should clamp to 1.0, got %v", res.Score)
	}
}

// --- LLMJudge 錯誤路徑 ---

func TestLLMJudgeErrors(t *testing.T) {
	// 空 rubric
	if _, err := (LLMJudge{}).Grade(context.Background(), Sample{}); err == nil {
		t.Error("empty rubric should error")
	}

	// mode=none
	judge := LLMJudge{Rubric: "r", Config: &config.LLMConfig{Mode: config.LLMModeNone}}
	if _, err := judge.Grade(context.Background(), Sample{}); err == nil {
		t.Error("mode=none should error")
	}

	// LLM 回非 JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"response": "I refuse to answer"})
	}))
	defer srv.Close()
	judge = LLMJudge{Rubric: "r", Config: &config.LLMConfig{
		Mode: config.LLMModeOllama, Ollama: config.OllamaConfig{URL: srv.URL, Model: "m", Timeout: 5},
	}}
	if _, err := judge.Grade(context.Background(), Sample{}); err == nil {
		t.Error("unparseable verdict should error")
	}
}

// --- SimilarityGrader（ollama embeddings）---

func TestSimilarityGraderOllama(t *testing.T) {
	// golden 與 output 回傳不同向量：cos([1,0,0],[0.6,0.8,0]) = 0.6
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		call++
		vec := []float64{1, 0, 0}
		if call > 1 {
			vec = []float64{0.6, 0.8, 0}
		}
		json.NewEncoder(w).Encode(map[string][]float64{"embedding": vec})
	}))
	defer srv.Close()

	g := SimilarityGrader{
		Golden: "expected answer",
		Config: &config.LLMConfig{
			Mode:   config.LLMModeOllama,
			Ollama: config.OllamaConfig{URL: srv.URL, Model: "embed-model", Timeout: 5},
		},
	}
	res, err := g.Grade(context.Background(), Sample{Output: "actual answer"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if math.Abs(res.Score-0.6) > 1e-9 {
		t.Errorf("score = %v, want 0.6", res.Score)
	}
	if res.Name != "similarity" {
		t.Errorf("Name = %q", res.Name)
	}

	if _, err := (SimilarityGrader{}).Grade(context.Background(), Sample{}); err == nil {
		t.Error("empty Golden should error")
	}
}
