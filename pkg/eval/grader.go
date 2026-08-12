// grader.go — Probabilistic Evaluation 核心介面
//
// Contract Testing 只回答「結構對了嗎」（binary pass/fail）；Grader 回答
// 「答案好嗎、有多好」（連續評分 0.0–1.0）。多個 Grader 的分數經
// Aggregate 加權聚合後與 PassThreshold 比較，見 pkg/contract 的
// TestCase.Graders / PassThreshold 整合。
package eval

import (
	"context"
	"fmt"
)

// Sample 一次評分的素材：路由、請求輸入與實際回應
type Sample struct {
	// Route "METHOD /path" 格式，供評分 prompt 提供上下文
	Route string

	// Input 請求 body（JSON 字串，可為空）
	Input string

	// Output handler 的實際回應 body
	Output string
}

// GraderResult 單一評判器的評分結果
type GraderResult struct {
	// Name 評判器名稱（同 Grader.Name()，作為 Scores map 的 key）
	Name string

	// Score 評分，0.0（最差）～ 1.0（最好）
	Score float64

	// Reason 評分理由（deterministic 評判器可為空）
	Reason string
}

// Grader 評判器介面
// SchemaGrader 為 deterministic（1.0 或 0.0）；LLMJudge / SimilarityGrader
// 為 probabilistic（連續分數）。實作必須是並行安全的（TestAll 可能並行呼叫）
type Grader interface {
	// Name 評判器名稱，作為分數記錄的 key（如 "schema"、"llm_judge"、"similarity"）
	Name() string

	// Grade 對一份 Sample 評分。回傳 error 表示評分本身失敗
	//（如 LLM 連線失敗），呼叫端應視為測試失敗而非低分
	Grade(ctx context.Context, s Sample) (GraderResult, error)
}

// SchemaGrader 將任意驗證函式包裝為 deterministic 評判器：
// 驗證通過 = 1.0，失敗 = 0.0（附失敗原因）
type SchemaGrader struct {
	// Validate 驗證函式，nil error 表示通過
	Validate func(output string) error
}

// Name 實作 Grader 介面
func (g SchemaGrader) Name() string { return "schema" }

// Grade 實作 Grader 介面
func (g SchemaGrader) Grade(_ context.Context, s Sample) (GraderResult, error) {
	if g.Validate == nil {
		return GraderResult{}, fmt.Errorf("eval: SchemaGrader.Validate is nil")
	}
	if err := g.Validate(s.Output); err != nil {
		return GraderResult{Name: g.Name(), Score: 0.0, Reason: err.Error()}, nil
	}
	return GraderResult{Name: g.Name(), Score: 1.0}, nil
}

// Aggregate 將多個評分結果加權平均為單一分數（0.0–1.0）。
// weights 以 GraderResult.Name 為 key；nil 或缺項時該評判器權重為 1（等權）。
// 無結果時回傳 0
func Aggregate(results []GraderResult, weights map[string]float64) float64 {
	var sum, totalWeight float64
	for _, r := range results {
		w := 1.0
		if weights != nil {
			if custom, ok := weights[r.Name]; ok {
				w = custom
			}
		}
		if w <= 0 {
			continue
		}
		sum += r.Score * w
		totalWeight += w
	}
	if totalWeight == 0 {
		return 0
	}
	return sum / totalWeight
}

// clampScore 將分數夾到 [0,1]（防 LLM 回傳超界值）
func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}
