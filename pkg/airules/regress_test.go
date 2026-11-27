package airules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maoxiaoyue/hypgo/pkg/contract"
	"github.com/maoxiaoyue/hypgo/pkg/errors"
	"github.com/maoxiaoyue/hypgo/pkg/hidb"
	"github.com/maoxiaoyue/hypgo/pkg/manifest"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// 規則裡寫的 API 都要存在：AI 會照抄。這裡編譯不過就表示規則要跟著改
var (
	_ = (*hidb.Database).ReadHypDB
	_ = (*hidb.Database).WriteHypDB
	_ = (*hidb.Database).HypDBTransaction
	_ = schema.RegisterGRPC
	_ = contract.Request
	_ = contract.TestAll
	_ = errors.Define
	_ = errors.AbortWithAppError
)

func allOn() Options {
	return withToggles(CommentToggles{NormalComment: true, AIComment: true, ThinkComment: true})
}

// 先前的內容 AI 照做會出錯：E1001 啟動 panic、hidb.ReadHypDB 編譯不過、指向不存在的檔案
func TestCoreContentHasNoHarmfulInstructions(t *testing.T) {
	out := coreContent(testManifest(), allOn())
	for _, bad := range []string{
		"E1001", "E2001", // 框架保留
		"hidb.ReadHypDB", "hidb.WriteHypDB", "hidb.HypDBTransaction", // 是方法，不是套件函式
		"AI_CODING_RULES", // 專案裡沒有這個檔案
		".hyp/manifest.yaml",
		"`.hyp/config.yaml`", // logger 不在那裡
		"Rules above marked MANDATORY",
		"Verify with `go vet", // go vet 不查註解
		"setupRouter",         // 生成的專案沒有這個函式
		"Log every error",     // Wrap 的底層錯誤已由 server 記錄
		"log.Error(",
		`"E_user_`, // scaffold 生成的資源：照抄會撞號
		`"E_auth_`,
		"400/422",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("rules still contain %q", bad)
		}
	}
	for _, want := range []string{
		`errors.Define("E_order_001"`,
		"ErrOrderInternal.Wrap(err)",
		"do not log it again in the handler",
		"400 / 413 / 415 / 422",
		"`validate:\"dive\"`",
		"`hyp testgen`",
		"func TestSetup_Contracts(t *testing.T)",
		"hyp lint --deep",
		"E0xxx-E2xxx are reserved",
		"`db.ReadHypDB()`",
		"`logger:` in `config/config.yaml`",
		"hyp context -o .hyp/context.yaml",
		"does not affect rules 1-4",
		"Web / API projects only",
		"`schema.RegisterGRPC(...)`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rules are missing %q", want)
		}
	}
}

func TestRouteTable(t *testing.T) {
	out := coreContent(&manifest.Manifest{Routes: []manifest.RouteManifest{
		{Method: "GET", Path: "/a", Summary: "pipe | inside"},
		{Method: "GET", Path: "/b"},
		{Protocol: "grpc", Command: "pkg.Svc/Do", Summary: "multi\nline\r\nsummary"},
		{Protocol: "bot", Platform: "telegram", Command: "/start", Summary: "bot"},
	}}, Options{})
	for _, want := range []string{
		`| GET | /a | pipe \| inside |`,
		"| GET | /b | - |",
		"| grpc | pkg.Svc/Do | multi line summary |",
		"| bot/telegram | /start | bot |",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing row %q in:\n%s", want, out[strings.Index(out, "## Current Routes"):])
		}
	}
}

func generateOne(t *testing.T, name string, m *manifest.Manifest, opts Options) string {
	t.Helper()
	targets, err := ResolveTargets(name)
	if err != nil || len(targets) != 1 {
		t.Fatalf("target %s: %v", name, err)
	}
	return targets[0].Generate(m, opts)
}

// frontmatter 必須在檔案開頭，標記緊接其後
func TestFrontmatterComesFirst(t *testing.T) {
	for name, want := range map[string]string{
		"cursor":   "alwaysApply: true\n",
		"windsurf": "trigger: always_on\n",
	} {
		out := generateOne(t, name, testManifest(), Options{})
		if !strings.HasPrefix(out, "---\n") {
			t.Errorf("%s: frontmatter is not first:\n%s", name, out[:60])
		}
		_, afterOpen, _ := strings.Cut(out, "---\n")
		frontmatter, rest, ok := strings.Cut(afterOpen, "---\n")
		if !ok || !strings.Contains(frontmatter, want) {
			t.Errorf("%s: frontmatter = %q", name, frontmatter)
		}
		if !strings.HasPrefix(rest, markerPrefix) {
			t.Errorf("%s: marker should follow the frontmatter, got %q", name, rest[:40])
		}
	}
}

func TestWindsurfStaysWithinLimit(t *testing.T) {
	diffLogAllOn := allOn()
	diffLogAllOn.DiffLogEnabled = true

	// 開關全開 + diff-log 的核心內容本身就要在上限內：路由表拿掉後沒有別的能砍
	if n := utf8.RuneCountInString(generateOne(t, "windsurf", nil, diffLogAllOn)); n > windsurfCharLimit {
		t.Errorf("core content is %d chars, limit %d", n, windsurfCharLimit)
	}

	// 路由表放不下時整段拿掉，檔案仍是有效的生成檔
	big := &manifest.Manifest{}
	for i := 0; i < maxRouteRows; i++ {
		big.Routes = append(big.Routes, manifest.RouteManifest{Method: "GET", Path: "/some/long/path", Summary: strings.Repeat("很長的路由說明", 30)})
	}
	out := generateOne(t, "windsurf", big, Options{})
	if n := utf8.RuneCountInString(out); n > windsurfCharLimit {
		t.Errorf("with many routes: %d chars", n)
	}
	if strings.Contains(out, "## Current Routes") {
		t.Error("route table should be dropped when it does not fit")
	}
	if classify([]byte(out)) != fileGenerated {
		t.Error("truncated output should still verify as generated")
	}

	// 放得下時保留
	if out := generateOne(t, "windsurf", testManifest(), Options{}); !strings.Contains(out, "## Current Routes") {
		t.Error("small route table should be kept")
	}
}

func TestResolveTargetsRejectsMistakes(t *testing.T) {
	for only, want := range map[string]string{
		"agents,cursro": `unknown target "cursro"`,
		"nonexistent":   "valid: ",
		"all,agents":    `"all" cannot be combined`,
		"every,agents":  `"every" cannot be combined`,
		"agents,*":      `"*" cannot be combined`,
	} {
		for name, resolve := range map[string]func(string) ([]Target, error){"generate": ResolveTargets, "clean": ResolveCleanTargets} {
			if _, err := resolve(only); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s %q: err = %v, want %q", name, only, err, want)
			}
		}
	}
	// 結尾多一個逗號不算錯
	if got, err := ResolveTargets("agents,"); err != nil || len(got) != 1 {
		t.Errorf("agents, : %v %v", got, err)
	}
}

func TestDefaultSetUsesAgentsNotChatGPTOrDeepSeek(t *testing.T) {
	names := map[string]bool{}
	for _, target := range DefaultTargets() {
		names[target.Name] = true
	}
	if !names["agents"] || names["chatgpt"] || names["deepseek"] {
		t.Errorf("default set = %v", names)
	}
	// 仍可明確指定
	for _, name := range []string{"chatgpt", "deepseek"} {
		if got, err := ResolveTargets(name); err != nil || len(got) != 1 {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
}

func TestClassify(t *testing.T) {
	gen := render("", "# Title\n\nbody\n")
	genFM := render("---\ntrigger: always_on\n---\n", "\n# Title\n")
	for name, tc := range map[string]struct {
		content string
		want    fileKind
	}{
		"generated":              {gen, fileGenerated},
		"generated frontmatter":  {genFM, fileGenerated},
		"crlf":                   {strings.ReplaceAll(gen, "\n", "\r\n"), fileGenerated},
		"bom":                    {"\xef\xbb\xbf" + gen, fileGenerated},
		"final newline added":    {gen + "\n", fileGenerated},
		"final newline removed":  {strings.TrimRight(gen, "\n"), fileGenerated},
		"edited":                 {gen + "MY EDIT\n", fileModified},
		"edited frontmatter":     {strings.Replace(genFM, "always_on", "manual", 1), fileModified},
		"legacy":                 {legacyMarker + "\n# old\n", fileLegacy},
		"legacy before fm":       {legacyMarker + "\n---\nglobs: x\n---\n# old\n", fileLegacy},
		"adopted after fm":       {"---\nglobs: x\n---\n" + legacyMarker + "\n# mine\n", fileLegacy},
		"manual":                 {"# Custom\n", fileManual},
		"mentions marker":        {"# Mine\n\nAdd " + legacyMarker + " to adopt.\n", fileManual},
		"marker on a later line": {"# Mine\n" + legacyMarker + "\n", fileManual},
		"marker in code block":   {"```\n" + legacyMarker + "\n```\n", fileManual},
		"bad hash format":        {markerPrefix + " sha256:zz -->\n# x\n", fileManual},
		"unclosed frontmatter":   {"---\nfoo\n" + legacyMarker + "\n", fileManual},
		"empty":                  {"", fileManual},
	} {
		if got := classify([]byte(tc.content)); got != tc.want {
			t.Errorf("%s: classify = %d, want %d", name, got, tc.want)
		}
	}
}

func agentsTarget(t *testing.T) []Target {
	t.Helper()
	got, err := ResolveTargets("agents")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestGenerateOutputIsStable(t *testing.T) {
	// 重跑位元組相同：AI 工具載入的檔案不會無故改變
	if a, b := generateOne(t, "claude", testManifest(), Options{}), generateOne(t, "claude", testManifest(), Options{}); a != b {
		t.Error("output differs between runs")
	}
}

// 手寫文件提到標記字串時不能被覆蓋或刪除
func TestManualFileMentioningMarkerIsKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const content = "# Mine\n\nAdd " + legacyMarker + " to adopt.\n"
	os.WriteFile(path, []byte(content), 0o644)

	opts := Options{Force: true} // force 也不動手寫檔
	results, _ := GenerateAll(dir, agentsTarget(t), testManifest(), opts, false)
	if results[0].Status != StatusSkipped {
		t.Errorf("generate: %s", results[0].Status)
	}
	results, _ = CleanAll(dir, agentsTarget(t), false, true)
	if results[0].Status != StatusSkipped {
		t.Errorf("clean: %s", results[0].Status)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf("file changed: %q", data)
	}
}

func TestEditedFileNeedsForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if _, err := GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	edited := string(data) + "\nMY EDIT\n"
	os.WriteFile(path, []byte(edited), 0o644)

	results, _ := GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false)
	if results[0].Status != StatusSkipped || !strings.Contains(results[0].Message, "--force") {
		t.Errorf("generate: %s %s", results[0].Status, results[0].Message)
	}
	results, _ = CleanAll(dir, agentsTarget(t), false, false)
	if results[0].Status != StatusSkipped || !strings.Contains(results[0].Message, "--force") {
		t.Errorf("clean: %s %s", results[0].Status, results[0].Message)
	}
	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatal("edited file was changed without --force")
	}

	results, _ = GenerateAll(dir, agentsTarget(t), testManifest(), Options{Force: true}, false)
	if results[0].Status != StatusCreated {
		t.Errorf("force generate: %s", results[0].Status)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "MY EDIT") {
		t.Error("--force should overwrite")
	}

	os.WriteFile(path, []byte(edited), 0o644)
	results, _ = CleanAll(dir, agentsTarget(t), false, true)
	if results[0].Status != StatusRemoved {
		t.Errorf("force clean: %s", results[0].Status)
	}
}

// 有標記、沒有 hash：無法確認內容沒被改過，生成與 clean 都要 force。
// 先前生成時一律覆寫
func TestLegacyMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(path, []byte(legacyMarker+"\n# old\n"), 0o644)

	results, _ := CleanAll(dir, agentsTarget(t), false, false)
	if results[0].Status != StatusSkipped {
		t.Errorf("clean without force: %s", results[0].Status)
	}

	results, _ = GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false)
	if results[0].Status != StatusSkipped || !results[0].NeedsForce || !strings.Contains(results[0].Message, "--force") {
		t.Errorf("generate without force: %+v", results[0])
	}
	if data, _ := os.ReadFile(path); string(data) != legacyMarker+"\n# old\n" {
		t.Errorf("file changed without force:\n%.80s", data)
	}

	results, _ = GenerateAll(dir, agentsTarget(t), testManifest(), Options{Force: true}, false)
	if results[0].Status != StatusCreated {
		t.Errorf("generate with force: %s", results[0].Status)
	}
	data, _ := os.ReadFile(path)
	if classify(data) != fileGenerated {
		t.Errorf("re-run should upgrade to the hashed marker:\n%.80s", data)
	}

	results, _ = CleanAll(dir, agentsTarget(t), false, false)
	if results[0].Status != StatusRemoved {
		t.Errorf("clean after upgrade: %s", results[0].Status)
	}
}

// 讀不了的檔案不能當成可以覆蓋
func TestUnreadablePathIsAnError(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755)
	results, _ := GenerateAll(dir, agentsTarget(t), nil, Options{}, false)
	if results[0].Status != StatusError {
		t.Errorf("status = %s", results[0].Status)
	}
}

// 先前把最後一行（hash）換成自己的筆記，下次生成筆記就被覆寫且沒有提示
func TestRemovedHashLineKeepsUserEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if _, err := GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	edited := strings.Join(lines[:len(lines)-1], "\n") + "\nMY NOTES\n"
	os.WriteFile(path, []byte(edited), 0o644)

	results, _ := GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false)
	if results[0].Status != StatusSkipped || !results[0].NeedsForce {
		t.Errorf("result = %+v", results[0])
	}
	if data, _ := os.ReadFile(path); string(data) != edited {
		t.Error("notes were overwritten")
	}
	// 手寫檔案的略過不需要 --force 的提示
	os.WriteFile(path, []byte("# mine\n"), 0o644)
	if results, _ := GenerateAll(dir, agentsTarget(t), testManifest(), Options{Force: true}, false); results[0].Status != StatusSkipped || results[0].NeedsForce {
		t.Errorf("manual file: %+v", results[0])
	}
}

// 先前內容沒變也重寫檔案並回報 created
func TestUnchangedFileIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	results, _ := GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false)
	if results[0].Status != StatusCreated {
		t.Fatalf("first run: %s", results[0].Status)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)

	results, _ = GenerateAll(dir, agentsTarget(t), testManifest(), Options{}, false)
	if results[0].Status != StatusUnchanged || results[0].Status.String() != "unchanged" {
		t.Errorf("second run: %s", results[0].Status)
	}
	if info, _ := os.Stat(path); info.ModTime().After(old.Add(time.Minute)) {
		t.Error("file was rewritten")
	}
	// 內容變了才寫
	results, _ = GenerateAll(dir, agentsTarget(t), nil, Options{}, false)
	if results[0].Status != StatusCreated {
		t.Errorf("after a change: %s", results[0].Status)
	}
}

// 先前路由表沒有上限：300 條路由讓每個每次都載入的檔案長到 20,000 字元
func TestRouteTableIsCapped(t *testing.T) {
	routes := func(n int) *manifest.Manifest {
		m := &manifest.Manifest{}
		for i := 0; i < n; i++ {
			m.Routes = append(m.Routes, manifest.RouteManifest{Method: "GET", Path: fmt.Sprintf("/r%d", i), Summary: "s"})
		}
		return m
	}
	at := coreContent(routes(maxRouteRows), Options{})
	if !strings.Contains(at, "| GET | /r0 | s |") || !strings.Contains(at, fmt.Sprintf("| GET | /r%d | s |", maxRouteRows-1)) {
		t.Error("the table should be kept at the limit")
	}
	over := coreContent(routes(maxRouteRows+1), Options{})
	if strings.Contains(over, "| GET |") {
		t.Error("the table should be left out above the limit")
	}
	if !strings.Contains(over, fmt.Sprintf("%d routes: see `routes` in `.hyp/context.yaml`", maxRouteRows+1)) {
		t.Errorf("no pointer to the manifest:\n%s", over[len(over)-200:])
	}
	if len(over) >= len(at) {
		t.Error("the capped output should be shorter")
	}
}

// 先前上限寫成 6000（全域規則的上限）：約 35 條路由就把路由表拿掉
func TestWindsurfKeepsRouteTableUnderLimit(t *testing.T) {
	m := &manifest.Manifest{}
	for i := 0; i < maxRouteRows; i++ {
		m.Routes = append(m.Routes, manifest.RouteManifest{Method: "GET", Path: fmt.Sprintf("/api/resources%d/:id", i), Summary: "Get resource by ID"})
	}
	out := generateOne(t, "windsurf", m, Options{})
	if n := utf8.RuneCountInString(out); n <= 6000 || n > windsurfCharLimit {
		t.Fatalf("test needs an output between 6000 and %d chars, got %d", windsurfCharLimit, n)
	}
	if !strings.Contains(out, "| GET | /api/resources0/:id |") {
		t.Error("route table dropped although it fits")
	}
}
