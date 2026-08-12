// similarity_grader.go — embedding 餘弦相似度評判器
package eval

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// SimilarityGrader 以 embedding 餘弦相似度比較實際回應與黃金標準（Golden）
//
// 用法：
//
//	eval.SimilarityGrader{Golden: "預期的理想回應內容", Config: llmCfg}
//
// ollama 模式走 /api/embeddings；api 模式走 OpenAI 相容 /embeddings
type SimilarityGrader struct {
	// Golden 黃金標準（預期的理想回應），與實際回應比相似度
	Golden string

	// Config LLM 連線設定（embedding 模型沿用其 model 欄位）
	Config *config.LLMConfig

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

	goldenVec, err := embedText(ctx, g.Config, g.Client, g.Golden)
	if err != nil {
		return GraderResult{}, err
	}
	outputVec, err := embedText(ctx, g.Config, g.Client, s.Output)
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
