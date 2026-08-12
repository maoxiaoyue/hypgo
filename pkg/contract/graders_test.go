package contract

import (
	"context"
	"fmt"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/eval"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// fakeGrader 固定分數的假評判器（測試 gating 邏輯用，不打網路）
type fakeGrader struct {
	name  string
	score float64
	err   error
}

func (f fakeGrader) Name() string { return f.name }
func (f fakeGrader) Grade(_ context.Context, _ eval.Sample) (eval.GraderResult, error) {
	if f.err != nil {
		return eval.GraderResult{}, f.err
	}
	return eval.GraderResult{Name: f.name, Score: f.score, Reason: "fixed"}, nil
}

func gradersTestRouter() *router.Router {
	schema.Global().Reset()
	r := router.New()
	r.GET("/ping", func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"status": "ok"})
	})
	return r
}

// TestGradersPassAboveThreshold 分數達門檻 → pass，且分數含 aggregate
func TestGradersPassAboveThreshold(t *testing.T) {
	r := gradersTestRouter()

	pass, reason, scores := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 200,
		Graders: []eval.Grader{
			fakeGrader{name: "a", score: 0.9},
			fakeGrader{name: "b", score: 0.7},
		},
		PassThreshold: 0.8,
	})
	if !pass {
		t.Fatalf("should pass (avg 0.8 >= 0.8), reason: %s", reason)
	}
	if scores["a"] != 0.9 || scores["b"] != 0.7 {
		t.Errorf("per-grader scores wrong: %v", scores)
	}
	if scores["aggregate"] != 0.8 {
		t.Errorf("aggregate = %v, want 0.8", scores["aggregate"])
	}
}

// TestGradersFailBelowThreshold 分數低於門檻 → fail，reason 含各評判器明細
func TestGradersFailBelowThreshold(t *testing.T) {
	r := gradersTestRouter()

	pass, reason, scores := runTestOnce(r, TestCase{
		Route:         "GET /ping",
		ExpectStatus:  200,
		Graders:       []eval.Grader{fakeGrader{name: "judge", score: 0.4}},
		PassThreshold: 0.8,
	})
	if pass {
		t.Fatal("should fail (0.4 < 0.8)")
	}
	if !strings.Contains(reason, "below threshold") || !strings.Contains(reason, "judge") {
		t.Errorf("reason should mention threshold and grader name: %q", reason)
	}
	if scores["aggregate"] != 0.4 {
		t.Errorf("aggregate = %v, want 0.4", scores["aggregate"])
	}
}

// TestGradersZeroThresholdReportsOnly 門檻 0 → 只記錄分數，不影響 pass/fail
func TestGradersZeroThresholdReportsOnly(t *testing.T) {
	r := gradersTestRouter()

	pass, _, scores := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 200,
		Graders:      []eval.Grader{fakeGrader{name: "judge", score: 0.1}},
		// PassThreshold 未設（0）
	})
	if !pass {
		t.Fatal("threshold 0 should never gate, even with low score")
	}
	if scores["judge"] != 0.1 {
		t.Errorf("scores should still be recorded: %v", scores)
	}
}

// TestGraderErrorFailsTest 評判器本身出錯（如 LLM 連線失敗）→ 測試失敗
func TestGraderErrorFailsTest(t *testing.T) {
	r := gradersTestRouter()

	pass, reason, _ := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 200,
		Graders:      []eval.Grader{fakeGrader{name: "judge", err: fmt.Errorf("llm unreachable")}},
	})
	if pass {
		t.Fatal("grader error should fail the test (no silent skip)")
	}
	if !strings.Contains(reason, "llm unreachable") {
		t.Errorf("reason should carry grader error: %q", reason)
	}
}

// TestGradersSkippedWhenDeterministicFails deterministic 檢查失敗時不執行評判器
func TestGradersSkippedWhenDeterministicFails(t *testing.T) {
	r := gradersTestRouter()

	graderCalled := false
	spy := eval.SchemaGrader{Validate: func(string) error {
		graderCalled = true
		return nil
	}}

	pass, _, scores := runTestOnce(r, TestCase{
		Route:        "GET /ping",
		ExpectStatus: 500, // 故意不符 → deterministic 失敗
		Graders:      []eval.Grader{spy},
	})
	if pass {
		t.Fatal("should fail on status mismatch")
	}
	if graderCalled {
		t.Error("graders should not run when deterministic checks fail")
	}
	if scores != nil {
		t.Errorf("scores should be nil, got %v", scores)
	}
}
