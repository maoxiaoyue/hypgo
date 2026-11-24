package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 不經 server.Start() 而一直執行的程式（gRPC 專案、main() 先阻塞）：到 --timeout 就停下並回錯。
// 歷史 bug：收集步驟是沒有逾時的 go run，hyp lint 永遠不結束，CI 卡死
func TestLintStopsAProjectThatKeepsRunning(t *testing.T) {
	migrateProject(t)
	// 像 gRPC 服務一樣監聽後阻塞
	os.WriteFile("main.go", []byte(`package main

import "net"

func main() {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	l.Accept()
}
`), 0o644)

	start := time.Now()
	out, err := runHypOutput(t, "lint", "--timeout", "2s")
	if err == nil {
		t.Fatal("a project that never exits passed the lint")
	}
	if took := time.Since(start); took > 90*time.Second {
		t.Errorf("gave up only after %s", took)
	}
	for _, want := range []string{"still running after 2s", "gRPC", "--no-refresh"} {
		if !strings.Contains(err.Error()+out, want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// main() 自己結束並回錯時說明原因在哪裡、怎麼繞過；成功時不印出專案的輸出。
// 歷史 bug：只有一行 "go run .: exit status 1"；每次 lint 都印出 "starting on :8080 ..."
func TestLintExplainsAFailingMainAndStaysQuiet(t *testing.T) {
	migrateProject(t)
	os.WriteFile("main.go", []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("database init failed: cannot reach db")
	os.Exit(1)
}
`), 0o644)
	_, err := runHypOutput(t, "lint")
	if err == nil {
		t.Fatal("a failing main() passed the lint")
	}
	for _, want := range []string{"exited with an error", "without a database", "--no-refresh"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}

	os.WriteFile("main.go", []byte(`package main

import (
	"fmt"

	"github.com/maoxiaoyue/hypgo"
)

func main() {
	fmt.Println("starting on :8080 (logs: logs/app.log)")
	hypgo.New().Run()
}
`), 0o644)
	stderr := captureStderr(t, func() {
		if err := runHyp(t, "lint"); err != nil {
			t.Errorf("lint: %v", err)
		}
	})
	if strings.Contains(stderr, "starting on") {
		t.Errorf("the project's own output was printed on a successful lint: %q", stderr)
	}
}

// captureStderr 收集 f 執行期間寫到 os.Stderr 的內容
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	f()
	os.Stderr = old
	w.Close()
	return <-done
}

// CLI 專案的 hyp lint 不改 .hyp/context.yaml；與專案同名的子命令不能生成。
// 歷史 bug：lint 經 tools/genctx 重寫 .hyp/context.yaml（說明文字寫「不寫檔」）；
// hyp generate command <專案名> 生成的專案一啟動就 panic（Schema 重複登錄）
func TestCLIProjectLintAndRootCommandName(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	if err := runHyp(t, "new", "cli", "mytool"); err != nil {
		t.Fatal(err)
	}
	t.Chdir("mytool")
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	os.WriteFile("go.sum", sum, 0o644)
	if out, err := exec.Command("go", "mod", "edit", "-replace", "github.com/maoxiaoyue/hypgo="+filepath.ToSlash(root)).CombinedOutput(); err != nil {
		t.Fatalf("go mod edit: %v\n%s", err, out)
	}

	if err := runHyp(t, "generate", "command", "mytool"); err == nil || !strings.Contains(err.Error(), "root command") {
		t.Errorf("generate command <project name>: %v", err)
	}
	if schemaSrc, _ := os.ReadFile(filepath.Join("app", "commands", "schema.go")); strings.Contains(string(schemaSrc), "請在此補登") {
		t.Error("the template still tells users to register subcommands a second time")
	}

	marker := []byte("# untouched\nlint:\n  total: 0\n  with_schema: 0\n  coverage: 0/0\n")
	os.MkdirAll(".hyp", 0o755)
	ctxFile := filepath.Join(".hyp", "context.yaml")
	os.WriteFile(ctxFile, marker, 0o644)
	if err := runHyp(t, "lint"); err != nil {
		t.Errorf("lint on a fresh CLI project: %v", err)
	}
	if got, _ := os.ReadFile(ctxFile); string(got) != string(marker) {
		t.Error("hyp lint rewrote .hyp/context.yaml in a CLI project")
	}
}
