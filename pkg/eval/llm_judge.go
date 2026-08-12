// llm_judge.go — LLM-as-judge 評判器
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// LLMJudge 以 LLM 依評分標準（Rubric）對回應評分（probabilistic）
//
// 用法：
//
//	contract.Test(t, r, contract.TestCase{
//	    Route: "POST /api/summarize", Input: `{"text":"..."}`,
//	    ExpectStatus: 200,
//	    Graders: []eval.Grader{
//	        eval.LLMJudge{Rubric: "摘要是否涵蓋原文重點且不捏造內容？", Config: llmCfg},
//	    },
//	    PassThreshold: 0.8,
//	})
type LLMJudge struct {
	// Rubric 評分標準（自然語言描述「好的回應長什麼樣」）
	Rubric string

	// Config LLM 連線設定（通常來自 config.LoadLLMConfig(".hyp/llm.yaml")）
	Config *config.LLMConfig

	// Client 可注入自訂 http.Client（測試用）；nil 時依 Config timeout 建立
	Client *http.Client
}

// Name 實作 Grader 介面
func (j LLMJudge) Name() string { return "llm_judge" }

// judgeVerdict LLM 評分回應的預期 JSON 形狀
type judgeVerdict struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Grade 實作 Grader 介面：組 prompt → 呼叫 LLM → 解析 {"score","reason"}
func (j LLMJudge) Grade(ctx context.Context, s Sample) (GraderResult, error) {
	if j.Rubric == "" {
		return GraderResult{}, fmt.Errorf("eval: LLMJudge.Rubric is empty")
	}

	prompt := fmt.Sprintf(`You are a strict quality judge for API responses.

Rubric (the standard a good response must meet):
%s

Route: %s
Request input:
%s

Actual response to evaluate:
%s

Score the response against the rubric. Respond with ONLY a JSON object, no other text:
{"score": <number between 0.0 and 1.0>, "reason": "<one-sentence justification>"}`,
		j.Rubric, s.Route, orEmpty(s.Input), s.Output)

	raw, err := completeText(ctx, j.Config, j.Client, prompt)
	if err != nil {
		return GraderResult{}, err
	}

	verdict, err := parseVerdict(raw)
	if err != nil {
		return GraderResult{}, fmt.Errorf("eval: LLMJudge unparseable verdict %q: %w", truncate(raw, 120), err)
	}

	return GraderResult{
		Name:   j.Name(),
		Score:  clampScore(verdict.Score),
		Reason: verdict.Reason,
	}, nil
}

// parseVerdict 解析 LLM 回應為 judgeVerdict。
// 模型偶爾會在 JSON 前後加說明文字，容錯：取第一個 '{' 到最後一個 '}' 之間
func parseVerdict(raw string) (judgeVerdict, error) {
	var v judgeVerdict
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		var v2 judgeVerdict
		if err := json.Unmarshal([]byte(raw[start:end+1]), &v2); err == nil {
			return v2, nil
		}
	}
	return judgeVerdict{}, fmt.Errorf("not a JSON object with score/reason")
}

func orEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
