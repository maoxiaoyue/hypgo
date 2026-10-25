package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// runHyp 以 rootCmd 執行一次 hyp 命令；panic 轉為 error，執行後把所有旗標恢復預設值
func runHyp(t *testing.T, args ...string) (err error) {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		resetFlags(rootCmd)
	}()
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if f.Changed {
			f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// offlineGo 讓 hyp new / hyp api 找不到 go 與 git：略過 go get @latest（不連網、不受 module cache 影響）
func offlineGo(t *testing.T) {
	t.Setenv("PATH", "")
}

// TestRunRestartRegisteredDockerRemoved 迴歸測試（A1）：run / restart 註冊的是真正的實作，docker 已移除。
// 歷史 bug：main.go 註冊了空的 Run stub，hyp run / restart / docker 什麼都不做就 exit 0
func TestRunRestartRegisteredDockerRemoved(t *testing.T) {
	found := map[string]*cobra.Command{}
	for _, c := range rootCmd.Commands() {
		found[c.Name()] = c
	}
	for _, name := range []string{"run", "restart"} {
		c := found[name]
		if c == nil || c.RunE == nil {
			t.Errorf("hyp %s is not wired to an implementation (cmd=%v)", name, c)
		}
	}
	if c := found["docker"]; c != nil {
		t.Errorf("hyp docker should be removed, still registered: %q", c.Short)
	}
}

// TestRestartFailsWithoutPIDFile 迴歸測試（A1）：沒有 hypgo.pid（或在 Windows 上）時 restart 回錯。
// 歷史 bug：Unix 上改以 pgrep -f main.go 找行程並送 SIGUSR2（會殺掉編輯器等無關行程）；Windows 印訊息後 exit 0
func TestRestartFailsWithoutPIDFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runHyp(t, "restart"); err == nil {
		t.Fatal("hyp restart should fail without hypgo.pid / on Windows")
	}
}

// TestRunDevRestartsOnChange 迴歸測試（A1）：hyp run 改檔後重建並重啟，舊行程結束、埠被釋放。
// 歷史 bug：以 go run 啟動並送 SIGTERM——Windows 不支援（cmd.Wait 卡住），Unix 只殺到 go run 包裝層，
// 真正的子行程繼續占著埠；而且只監看 app/ 與 config/
func TestRunDevRestartsOnChange(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	dir := t.TempDir()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	writeApp := func(marker string) {
		src := "package main\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n\t\"os\"\n)\n\n" +
			"func main() {\n" +
			"\thttp.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, \"" + marker + "\") })\n" +
			"\tif err := http.ListenAndServe(os.Getenv(\"DEVAPP_ADDR\"), nil); err != nil {\n" +
			"\t\tfmt.Fprintln(os.Stderr, err)\n\t\tos.Exit(1)\n\t}\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module devapp\n\ngo 1.24\n"), 0644)
	writeApp("v1")
	t.Setenv("DEVAPP_ADDR", addr)
	t.Chdir(dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runDev(ctx, ".", true) }()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if resp, err := http.Get("http://" + addr + "/"); err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if string(body) == want {
					return
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("app never served %q", want)
	}
	waitFor("v1")
	writeApp("v2")
	waitFor("v2") // 舊行程未釋放埠時，新行程 Listen 失敗，永遠等不到 v2

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runDev: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runDev did not stop after cancel")
	}
	l2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port still held after hyp run stopped: %v", err)
	}
	l2.Close()
}

// TestRunWatchesProjectFiles 迴歸測試（A1）：hyp run 監看專案的 .go / yaml（含 routers/、main.go），略過 .git、logs 等。
// 歷史 bug：只監看 app/ 與 config/，改 routers/ 或 main.go 不會重啟
func TestRunWatchesProjectFiles(t *testing.T) {
	for _, p := range []string{"main.go", filepath.Join("routers", "router.go"), filepath.Join("config", "config.yaml"), "go.mod"} {
		if !isWatchedFile(p) {
			t.Errorf("%s should trigger a rebuild", p)
		}
	}
	for _, p := range []string{"main_test.go", "hypgo.pid", filepath.Join("static", "app.js")} {
		if isWatchedFile(p) {
			t.Errorf("%s should not trigger a rebuild", p)
		}
	}
	for _, d := range []string{".git", ".hyp", "vendor", "logs", "tmp", "node_modules"} {
		if !skipWatchDir(d) {
			t.Errorf("directory %s should not be watched", d)
		}
	}
	for _, d := range []string{"routers", "app", "config"} {
		if skipWatchDir(d) {
			t.Errorf("directory %s should be watched", d)
		}
	}
}

// TestNewRefusesNonEmptyDir 迴歸測試（A2）：hyp new / hyp api 遇到非空的目標目錄時拒絕，不截斷既有檔案。
// 歷史 bug：不檢查目標目錄，對既有專案重跑會覆寫 go.mod、config.yaml 後才在半途失敗
func TestNewRefusesNonEmptyDir(t *testing.T) {
	offlineGo(t)
	for _, args := range [][]string{{"new", "proj"}, {"api", "proj"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Chdir(t.TempDir())
			os.MkdirAll(filepath.Join("proj", "config"), 0755)
			os.WriteFile(filepath.Join("proj", "go.mod"), []byte("module keep\n"), 0644)
			os.WriteFile(filepath.Join("proj", "config", "config.yaml"), []byte("keep: true\n"), 0644)

			if err := runHyp(t, args...); err == nil {
				t.Error("expected an error for a non-empty target directory")
			}
			for path, want := range map[string]string{"go.mod": "module keep\n", filepath.Join("config", "config.yaml"): "keep: true\n"} {
				if got, _ := os.ReadFile(filepath.Join("proj", path)); string(got) != want {
					t.Errorf("%s was overwritten: %q", path, got)
				}
			}
		})
	}
}

// TestNewRejectsInvalidProjectName 迴歸測試（A3）：專案名稱必須是單一目錄名稱且為合法的 module 路徑。
// 歷史 bug：hyp new ../escaped 寫到 CWD 之外並產生 module ../escaped；"my app" 產生 module my app
func TestNewRejectsInvalidProjectName(t *testing.T) {
	offlineGo(t)
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	os.MkdirAll(inner, 0755)
	t.Chdir(inner)
	for _, args := range [][]string{{"new", "../escaped"}, {"api", "../escaped"}, {"new", "my app"}, {"api", "my app"}, {"new", "cli", "../escaped"}} {
		if err := runHyp(t, args...); err == nil {
			t.Errorf("hyp %v should fail", args)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); err == nil {
		t.Error("project was written outside the working directory")
	}
	if _, err := os.Stat("my app"); err == nil {
		t.Error(`project "my app" was created`)
	}
}

// TestNewGoModUsesCLIVersion 迴歸測試（A4）：無法升級到 @latest 時，go.mod 使用 hyp 本身的版本。
// 歷史 bug：以 SSH git ls-remote 查 tag，失敗時退回 v0.8.5（沒有根 hypgo 套件，專案永遠編不過）
func TestNewGoModUsesCLIVersion(t *testing.T) {
	offlineGo(t)
	t.Chdir(t.TempDir())
	if err := runHyp(t, "new", "webapp"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("webapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "github.com/maoxiaoyue/hypgo v" + Version; !strings.Contains(string(data), want) {
		t.Errorf("go.mod should require %s:\n%s", want, data)
	}
}

// TestMigrateDiffFailsWhenProjectCannotRun 迴歸測試（A5）：無法執行專案時 hyp migrate diff / snapshot 回錯。
// 歷史 bug：只讀 hyp 自己行程的 GlobalRegistry，印「No models registered」並 exit 0
func TestMigrateDiffFailsWhenProjectCannotRun(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, sub := range []string{"diff", "snapshot"} {
		if err := runHyp(t, "migrate", sub); err == nil {
			t.Errorf("hyp migrate %s should fail when the project cannot be run", sub)
		}
	}
}

// TestMigrateDiffRunsProject 迴歸測試（A5）：hyp migrate diff 執行使用者專案，以其註冊的 model 產生 CREATE TABLE。
// 歷史 bug：使用者的 model 只註冊在專案行程裡，hyp 永遠看不到
func TestMigrateDiffRunsProject(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a project against the local hypgo module")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module widgetapp\n\ngo 1.24\n\n"+
		"require github.com/maoxiaoyue/hypgo v0.0.0\n\n"+
		"replace github.com/maoxiaoyue/hypgo => "+filepath.ToSlash(root)+"\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"github.com/maoxiaoyue/hypgo"
	"github.com/maoxiaoyue/hypgo/pkg/migrate"
	"github.com/uptrace/bun"
)

type Widget struct {
	bun.BaseModel `+"`bun:\"table:widgets\"`"+`

	ID   int64  `+"`bun:\"id,pk,autoincrement\"`"+`
	Name string `+"`bun:\"name,notnull\"`"+`
}

func init() { migrate.GlobalRegistry().Register((*Widget)(nil)) }

func main() {
	if err := hypgo.New().Run(); err != nil {
		panic(err)
	}
}
`), 0644)
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)

	if err := runHyp(t, "migrate", "diff", "-o", "migs"); err != nil {
		t.Fatal(err)
	}
	ups, _ := filepath.Glob(filepath.Join("migs", "*_auto.up.sql"))
	if len(ups) != 1 {
		t.Fatalf("expected one up migration, got %v", ups)
	}
	up, _ := os.ReadFile(ups[0])
	if !strings.Contains(string(up), `CREATE TABLE IF NOT EXISTS "widgets"`) || !strings.Contains(string(up), `"name" TEXT NOT NULL`) {
		t.Errorf("up migration missing CREATE TABLE widgets:\n%s", up)
	}
	if _, err := os.Stat(filepath.Join(".hyp", "schema_snapshot.json")); err != nil {
		t.Errorf("snapshot not written: %v", err)
	}
}

// healthConfig 在 CWD 寫入 config/config.yaml
func healthConfig(t *testing.T, addr string, tls bool) {
	t.Helper()
	os.MkdirAll("config", 0755)
	cfg := fmt.Sprintf("server:\n  addr: %s\n  tls:\n    enabled: %v\n", addr, tls)
	if err := os.WriteFile(filepath.Join("config", "config.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestHealthHostPortAndPaths 迴歸測試（A6）：server.addr 帶主機時正確解析，未指定 --path 時 /health 404 會再試 /api/health。
// 歷史 bug：以 addr[1:] 取埠號（127.0.0.1:9999 → localhost:27.0.0.1:9999），路徑寫死 /api/health，並直接 os.Exit
func TestHealthHostPortAndPaths(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":true}`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Chdir(t.TempDir())
	healthConfig(t, strings.TrimPrefix(srv.URL, "http://"), false)
	if err := runHyp(t, "health"); err != nil {
		t.Errorf("web project (/api/health): %v", err)
	}
	if err := runHyp(t, "health", "--path", "/missing"); err == nil {
		t.Error("an explicit --path returning 404 should fail")
	}
}

// TestHealthUsesTLS 迴歸測試（A6）：server.tls.enabled 時以 https 檢查（loopback 允許自簽憑證）。
// 歷史 bug：一律用 http，TLS 專案永遠檢查失敗
func TestHealthUsesTLS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ok"}`)) })
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	t.Chdir(t.TempDir())
	healthConfig(t, strings.TrimPrefix(srv.URL, "https://"), true)
	if err := runHyp(t, "health"); err != nil {
		t.Errorf("TLS health check: %v", err)
	}
}

// TestGenerateEmptyNameNoPanic 迴歸測試（A7）：hyp generate 的空名稱回錯而非 panic。
// 歷史 bug：先取 name[:1] 才驗證，空字串直接 panic
func TestGenerateEmptyNameNoPanic(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, kind := range []string{"controller", "model", "service", "command", "view", "proto"} {
		err := runHyp(t, "generate", kind, "")
		if err == nil || strings.HasPrefix(err.Error(), "panic") {
			t.Errorf("hyp generate %s \"\": want a validation error, got %v", kind, err)
		}
	}
}

// TestAPIDockerfileMatchesGoDirective 迴歸測試（A8）：hyp api 的 Dockerfile 基底映像與 go.mod 的 go 版本一致。
// 歷史 bug：FROM golang:1.21-alpine 但 go.mod 為 go 1.24（官方映像 GOTOOLCHAIN=local，go mod download 失敗）
func TestAPIDockerfileMatchesGoDirective(t *testing.T) {
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindStringSubmatch(goModContent)
	if m == nil {
		t.Fatal("no go directive in goModContent")
	}
	if want := "FROM golang:" + m[1] + "-alpine"; !strings.Contains(dockerfileContent, want) {
		t.Errorf("Dockerfile should use %q", want)
	}
}

// TestLintFailsWithoutLintSection 迴歸測試（B1）：manifest 沒有 lint 區段時 hyp lint 失敗。
// 歷史 bug：印「Nothing to check」並 exit 0，CI gate 靜默通過
func TestLintFailsWithoutLintSection(t *testing.T) {
	t.Chdir(t.TempDir())
	os.MkdirAll(".hyp", 0755)
	os.WriteFile(filepath.Join(".hyp", "context.yaml"), []byte("routes: []\n"), 0644)
	if err := runHyp(t, "lint"); err == nil {
		t.Error("hyp lint should fail when the manifest has no lint section")
	}
}

// TestLintRejectsPackagesWithoutDeep 迴歸測試（B1）：未帶 --deep 時給套件參數回錯。
// 歷史 bug：套件參數被靜默忽略
func TestLintRejectsPackagesWithoutDeep(t *testing.T) {
	t.Chdir(t.TempDir())
	os.MkdirAll(".hyp", 0755)
	os.WriteFile(filepath.Join(".hyp", "context.yaml"), []byte("lint:\n  total: 0\n  with_schema: 0\n  coverage: 0/0\n"), 0644)
	if err := runHyp(t, "lint", "./pkg/..."); err == nil {
		t.Error("hyp lint ./pkg/... without --deep should fail")
	}
}

// TestNewWebRoutesUseSchema 迴歸測試（B2）：hyp new 的路由全部以 r.Schema(...).Handle 註冊。
// 歷史 bug：首頁以 r.GET("/", home.Home) 註冊、沒有 Schema，全新專案的 hyp lint 即失敗
func TestNewWebRoutesUseSchema(t *testing.T) {
	offlineGo(t)
	t.Chdir(t.TempDir())
	if err := runHyp(t, "new", "webapp"); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("webapp", "routers", "router.go"))
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`\br\.(GET|POST|PUT|PATCH|DELETE|Any|Handle)\(`).Find(src); m != nil {
		t.Errorf("route registered without Schema: %s", m)
	}
	if !strings.Contains(string(src), "Path:       \"/\",") {
		t.Errorf("home route should be declared with r.Schema:\n%s", src)
	}
}

// TestAIRulesDiffLogReadsDir 迴歸測試（B3）：ai-rules 讀 --dir 底下的 .hyp/config.json 決定是否寫入 diff-log 指令。
// 歷史 bug：一律讀 CWD 的 .hyp/config.json
func TestAIRulesDiffLogReadsDir(t *testing.T) {
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".hyp"), 0755)
	os.WriteFile(filepath.Join(proj, ".hyp", "config.json"), []byte(`{"diff_log": true}`), 0644)
	t.Chdir(t.TempDir())
	if err := runHyp(t, "ai-rules", "--dir", proj, "--only", "claude"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	if !strings.Contains(string(data), "hyp diff-log") {
		t.Error("diff_log enabled in --dir project, but CLAUDE.md has no diff-log instruction")
	}
}

// TestAIRulesWriteErrorFails 迴歸測試（B4）：ai-rules 寫檔失敗時回錯。
// 歷史 bug：錯誤只印到 stderr，仍 exit 0
func TestAIRulesWriteErrorFails(t *testing.T) {
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, "CLAUDE.md"), 0755) // 目標路徑是目錄 → 寫檔必定失敗
	if err := runHyp(t, "ai-rules", "--dir", proj, "--only", "claude"); err == nil {
		t.Error("hyp ai-rules should fail when a file cannot be written")
	}
}

// TestContextRejectsUnknownFormat 迴歸測試（B7）：hyp context -f 只接受 yaml / json。
// 歷史 bug：未知格式靜默改輸出 YAML
func TestContextRejectsUnknownFormat(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runHyp(t, "context", "-f", "xml")
	if err == nil || !strings.Contains(err.Error(), "format") {
		t.Errorf("want an unknown-format error, got %v", err)
	}
}

// TestTestgenFileShorthand 迴歸測試（B8）：hyp testgen 提供 wiki 記載的 -f 縮寫。
// 歷史 bug：wiki 記載 -f, --file，實際只有 --file
func TestTestgenFileShorthand(t *testing.T) {
	if f := testgenCmd.Flags().ShorthandLookup("f"); f == nil || f.Name != "file" {
		t.Errorf("-f should be the shorthand of --file, got %v", f)
	}
}
