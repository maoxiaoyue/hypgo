// similarity_grader.go — embedding 餘弦相似度評判器
package eval

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// SimilarityGrader 以 embedding 餘弦相似度比較實際回應與黃金標準（Golden）
//
// 用法：
//
//	eval.SimilarityGrader{Golden: "預期的理想回應內容", Config: llmCfg}
//
// ollama 模式走 /api/embeddings；api 模式走 OpenAI 相容 /embeddings。
// 不要直接共用 LLMJudge 的設定：chat 模型不能產生 embedding，要設 EmbeddingModel；
// provider 為 anthropic 時沒有 embeddings API，要另給一份 Config
type SimilarityGrader struct {
	// Golden 黃金標準（預期的理想回應），與實際回應比相似度
	Golden string

	// Config LLM 連線設定
	Config *config.LLMConfig

	// EmbeddingModel embedding 模型名稱（如 "text-embedding-3-small"）；留空時沿用 Config 的 model
	EmbeddingModel string

	// Client 可注入自訂 http.Client（測試用）
	Client *http.Client
}

// Name 實作 Grader 介面
func (g SimilarityGrader) Name() string { return "similarity" }

// Grade 實作 Grader 介面：embed(Golden) 與 embed(Output) 的餘弦相似度，
// 夾到 [0,1] 作為分數
func (g SimilarityGrader) Grade(ctx context.Context, s Sample) (GraderResult, error) {
	if g.Golden == "" {
		return GraderResult{}, fmt.Errorf("eval: SimilarityGrader.Golden is empty")
	}

	// 空的回應沒有內容可比：直接 0 分（先前照樣送出 {"input": ""}，得到的是 API 錯誤而不是分數）
	if strings.TrimSpace(s.Output) == "" {
		return GraderResult{Name: g.Name(), Score: 0, Reason: "empty response"}, nil
	}
	goldenVec, err := g.goldenEmbedding(ctx)
	if err != nil {
		return GraderResult{}, err
	}
	outputVec, err := embedText(ctx, g.Config, g.Client, g.EmbeddingModel, s.Output)
	if err != nil {
		return GraderResult{}, err
	}

	cos, err := cosineSimilarity(goldenVec, outputVec)
	if err != nil {
		return GraderResult{}, err
	}

	return GraderResult{
		Name:   g.Name(),
		Score:  clampScore(cos),
		Reason: fmt.Sprintf("cosine similarity to golden: %.4f", cos),
	}, nil
}

// goldenCache 快取 Golden 的 embedding（同一個行程內）：它每次都一樣，
// 先前每次評分都重新請求一次，一個測試兩個請求
var goldenCache sync.Map // 連線設定 + 模型 + Golden → []float64

// goldenEmbedding 回傳 Golden 的 embedding；注入了 Client（測試）時不快取
func (g SimilarityGrader) goldenEmbedding(ctx context.Context) ([]float64, error) {
	if g.Client != nil || g.Config == nil {
		return embedText(ctx, g.Config, g.Client, g.EmbeddingModel, g.Golden)
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", g.Config.Mode, g.Config.Ollama.URL, g.Config.Ollama.Model,
		g.Config.API.BaseURL, g.Config.API.Model, g.EmbeddingModel, g.Golden)
	if v, ok := goldenCache.Load(key); ok {
		return v.([]float64), nil
	}
	vec, err := embedText(ctx, g.Config, g.Client, g.EmbeddingModel, g.Golden)
	if err == nil {
		goldenCache.Store(key, vec)
	}
	return vec, err
}

// cosineSimilarity 兩向量的餘弦相似度（-1～1）
func cosineSimilarity(a, b []float64) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("eval: embedding dimension mismatch (%d vs %d)", len(a), len(b))
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("eval: zero-magnitude embedding vector")
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), nil
}
