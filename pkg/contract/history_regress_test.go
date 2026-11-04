package contract

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/eval"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// modulePackageDir 建立暫存模組並切到其中的套件目錄（go test 的工作目錄就是套件目錄），回傳模組根目錄
func modulePackageDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "routers")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkg)
	return root
}

func historyAt(t *testing.T, root string) []eval.EvalRecord {
	t.Helper()
	records, err := eval.LoadHistory(filepath.Join(root, ".hyp", "eval_history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func schemaRouter(t *testing.T) *router.Router {
	t.Helper()
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/ping", Summary: "ping"}).Handle(func(c *hypcontext.Context) {
		c.JSON(200, map[string]string{"s": "ok"})
	})
	return r
}

// 紀錄與報告都在模組根目錄，不是套件目錄
func TestHistoryAndObserveGoToModuleRoot(t *testing.T) {
	root := modulePackageDir(t)
	r := schemaRouter(t)

	TestAll(t, r, RunConfig{RecordHistory: true})
	ObserveAll(t, r)

	if got := historyAt(t, root); len(got) != 1 || got[0].Route != "GET /ping" {
		t.Errorf("history at module root = %+v", got)
	}
	reports, _ := filepath.Glob(filepath.Join(root, ".hyp", "observe_*.html"))
	if len(reports) != 1 {
		t.Errorf("observe reports at module root = %v", reports)
	}
	if _, err := os.Stat(".hyp"); err == nil {
		t.Error("package directory got a stray .hyp")
	}
}

// Test 能把 Grader 分數寫進歷史：先前 Test 不寫、TestAll 不能掛 Graders，分數永遠進不了歷史
func TestTestCaseRecordHistoryStoresGraderScores(t *testing.T) {
	root := modulePackageDir(t)
	r := schemaRouter(t)

	Test(t, r, TestCase{
		Route: "GET /ping", ExpectStatus: 200, RecordHistory: true,
		Graders: []eval.Grader{fakeGrader{name: "llm_judge", score: 0.4}},
	})

	got := historyAt(t, root)
	if len(got) != 1 {
		t.Fatalf("records = %+v", got)
	}
	rec := got[0]
	if rec.Status != "pass" || rec.Attempts != 1 || rec.Route != "GET /ping" {
		t.Errorf("record = %+v", rec)
	}
	if rec.Scores["contract"] != 1 || rec.Scores["llm_judge"] != 0.4 || rec.Scores["aggregate"] != 0.4 {
		t.Errorf("scores = %v", rec.Scores)
	}
}

func TestTestCaseWithoutRecordHistoryWritesNothing(t *testing.T) {
	root := modulePackageDir(t)
	Test(t, schemaRouter(t), TestCase{Route: "GET /ping", ExpectStatus: 200})
	if got := historyAt(t, root); len(got) != 0 {
		t.Errorf("records = %+v", got)
	}
}

// 重試通過要留下痕跡：先前只記 status:pass，不穩定被藏起來
func TestAllRecordsAttempts(t *testing.T) {
	root := modulePackageDir(t)
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	calls := 0
	r := router.New()
	r.Schema(schema.Route{Method: "GET", Path: "/flaky", Summary: "s"}).Handle(func(c *hypcontext.Context) {
		calls++
		if calls == 1 {
			c.JSON(500, map[string]string{"e": "x"})
			return
		}
		c.JSON(200, map[string]string{"s": "ok"})
	})

	TestAll(t, r, RunConfig{RetryCount: 2, RecordHistory: true})

	got := historyAt(t, root)
	if len(got) != 1 || got[0].Status != "pass" || got[0].Attempts != 2 {
		t.Errorf("records = %+v", got)
	}
}

func TestAllRecordsNonRESTRoutes(t *testing.T) {
	root := modulePackageDir(t)
	schema.Global().Reset()
	t.Cleanup(schema.Global().Reset)
	schema.Global().Register(schema.Route{Protocol: "mcp", Command: "search", Summary: "s"})

	TestAll(t, router.New(), RunConfig{RecordHistory: true})

	got := historyAt(t, root)
	if len(got) != 1 || got[0].Route != "mcp|search" || got[0].Status != "pass" {
		t.Errorf("records = %+v", got)
	}
}

// 要求記錄卻寫不進去要讓測試失敗。在子行程裡重跑：這個失敗是預期的，不能算到外層測試
func TestRecordHistoryWriteFailureFailsTheTest(t *testing.T) {
	const env = "HYPGO_TEST_HISTORY_WRITE_FAILURE"
	if os.Getenv(env) != "" {
		t.Chdir(t.TempDir())
		if err := os.WriteFile(".hyp", []byte("a file, not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		TestAll(t, schemaRouter(t), RunConfig{RecordHistory: true})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecordHistoryWriteFailureFailsTheTest$")
	cmd.Env = append(os.Environ(), env+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("test should fail when history cannot be written:\n%s", out)
	}
	if !strings.Contains(string(out), "record history") {
		t.Errorf("output:\n%s", out)
	}
}

func TestGradersInvalidScoreFails(t *testing.T) {
	r := gradersTestRouter()
	for _, score := range []float64{math.NaN(), math.Inf(1), 1.5, -0.1} {
		pass, reason, scores := runTestOnce(r, TestCase{
			Route: "GET /ping", ExpectStatus: 200, PassThreshold: 0.8,
			Graders: []eval.Grader{fakeGrader{name: "custom", score: score}},
		})
		if pass || !strings.Contains(reason, "invalid score") {
			t.Errorf("score %v: pass = %v, reason = %q", score, pass, reason)
		}
		// NaN 進到 scores 會讓整筆紀錄寫不進 JSON
		if _, ok := scores["custom"]; ok {
			t.Errorf("score %v was recorded: %v", score, scores)
		}
	}
}

type namedGrader struct {
	name, result string
	score        float64
}

func (g namedGrader) Name() string { return g.name }
func (g namedGrader) Grade(context.Context, eval.Sample) (eval.GraderResult, error) {
	return eval.GraderResult{Name: g.result, Score: g.score}, nil
}

func TestGraderNamesDoNotCollide(t *testing.T) {
	_, _, scores := runTestOnce(gradersTestRouter(), TestCase{
		Route: "GET /ping", ExpectStatus: 200,
		Graders: []eval.Grader{
			fakeGrader{name: "llm_judge", score: 0.2},
			fakeGrader{name: "llm_judge", score: 0.9},
			fakeGrader{name: "llm_judge", score: 0.4},
			fakeGrader{name: "contract", score: 0.5},
			fakeGrader{name: "aggregate", score: 0.5},
			namedGrader{name: "fallback", score: 0.5}, // 結果沒有名稱時用 Grader.Name()
		},
	})
	for name, want := range map[string]float64{
		"llm_judge": 0.2, "llm_judge#2": 0.9, "llm_judge#3": 0.4,
		"contract#2": 0.5, "aggregate#2": 0.5, "fallback": 0.5,
	} {
		if got, ok := scores[name]; !ok || got != want {
			t.Errorf("scores[%q] = %v, want %v (%v)", name, got, want, scores)
		}
	}
	if math.Abs(scores["aggregate"]-0.5) > 1e-9 {
		t.Errorf("aggregate = %v", scores["aggregate"])
	}
}
