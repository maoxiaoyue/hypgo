package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// devApp 在暫存目錄建立一個小型 HTTP 應用並切換過去：GET / 回 "<marker> <pid>"，
// 啟動時把自己的 PID 寫進 hypgo.pid（不自行清除），收到中斷訊號時寫下 stopped 檔再結束
type devApp struct {
	t    *testing.T
	dir  string
	addr string
}

func newDevApp(t *testing.T) *devApp {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := &devApp{t: t, dir: t.TempDir(), addr: l.Addr().String()}
	l.Close()
	if err := os.WriteFile(filepath.Join(a.dir, "go.mod"), []byte("module devapp\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a.write("v1")
	t.Setenv("DEVAPP_ADDR", a.addr)
	t.Chdir(a.dir)
	return a
}

func (a *devApp) write(marker string) {
	a.t.Helper()
	src := `package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	os.WriteFile("hypgo.pid", []byte(strconv.Itoa(os.Getpid())), 0644)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		os.WriteFile("stopped", []byte("` + marker + `"), 0644)
		os.Exit(0)
	}()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "` + marker + ` ", os.Getpid()) })
	if err := http.ListenAndServe(os.Getenv("DEVAPP_ADDR"), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(filepath.Join(a.dir, "main.go"), []byte(src), 0644); err != nil {
		a.t.Fatal(err)
	}
}

// get 回傳目前的回應（"<marker> <pid>"），連不上時為空字串
func (a *devApp) get() string {
	resp, err := http.Get("http://" + a.addr + "/")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// waitFor 等到回應符合 ok，回傳該回應
func (a *devApp) waitFor(what string, d time.Duration, ok func(string) bool) string {
	a.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if body := a.get(); body != "" && ok(body) {
			return body
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.t.Fatalf("%s: last response %q", what, a.get())
	return ""
}

// start 啟動 runDev，回傳結束它並等它返回的函式
func (a *devApp) start() (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDev(ctx, ".", true, nil) }()
	a.t.Cleanup(cancel)
	return func() {
		a.t.Helper()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				a.t.Errorf("runDev: %v", err)
			}
		case <-time.After(30 * time.Second):
			a.t.Fatal("runDev did not stop after cancel")
		}
	}
}

// TestRunDevSurvivesNestedNewDirs：一次建立多層目錄並寫入檔案之後，熱重載照常運作，
// 新目錄裡先寫好的檔案也會觸發重建。
// 先前主迴圈在處理目錄的 Create 事件時呼叫 w.Add，Windows 上與 fsnotify 讀取事件的 goroutine
// 互等，之後任何變更都不再重啟；其他平台則是新目錄裡搶在監看之前寫入的檔案沒有事件
func TestRunDevSurvivesNestedNewDirs(t *testing.T) {
	a := newDevApp(t)
	stop := a.start()
	first := a.waitFor("app never started", 60*time.Second, func(b string) bool { return strings.HasPrefix(b, "v1 ") })

	sub := filepath.Join(a.dir, "app", "newpkg", "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.go"), []byte("package sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	second := a.waitFor("a .go file written into a new nested directory did not restart the app", 30*time.Second,
		func(b string) bool { return b != first })

	// 新目錄確實被監看：之後改裡面的檔案也會重啟
	time.Sleep(time.Second)
	if err := os.WriteFile(filepath.Join(sub, "x.go"), []byte("package sub\n\nconst X = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	a.waitFor("a change inside the new directory did not restart the app", 30*time.Second,
		func(b string) bool { return b != first && b != second })

	a.write("v2")
	a.waitFor("hot reload stopped working after directories were created", 30*time.Second,
		func(b string) bool { return strings.HasPrefix(b, "v2 ") })
	stop()
}

// TestRunDevStopsAppGracefully：重載與結束時應用程式收到中斷訊號（有機會優雅關閉），
// 它沒清掉的 hypgo.pid 由 hyp run 移除。
// 先前 Windows 一律 Process.Kill：OnShutdown、defer、進行中的請求都被跳過，PID 檔留在原地
func TestRunDevStopsAppGracefully(t *testing.T) {
	a := newDevApp(t)
	if !canInterruptChild() {
		t.Skip("no console to send Ctrl+Break from")
	}
	stop := a.start()
	a.waitFor("app never started", 60*time.Second, func(b string) bool { return strings.HasPrefix(b, "v1 ") })

	a.write("v2")
	a.waitFor("app was not reloaded", 60*time.Second, func(b string) bool { return strings.HasPrefix(b, "v2 ") })
	if data, err := os.ReadFile("stopped"); err != nil || string(data) != "v1" {
		t.Errorf("the old process was not asked to stop before the reload (stopped=%q, err=%v)", data, err)
	}

	stop()
	if data, _ := os.ReadFile("stopped"); string(data) != "v2" {
		t.Errorf("the app was not asked to stop when hyp run ended (stopped=%q)", data)
	}
	if data, err := os.ReadFile("hypgo.pid"); err == nil {
		t.Errorf("hypgo.pid still records PID %s after the app has stopped", data)
	}
}

// TestRunWatchesTemplatesNotHiddenFiles：.html / .tmpl 觸發重建（web 模板以 go:embed 編進執行檔），
// 點開頭的檔案不觸發（Emacs 的鎖檔 .#main.go）
func TestRunWatchesTemplatesNotHiddenFiles(t *testing.T) {
	for _, p := range []string{filepath.Join("templates", "welcome.html"), filepath.Join("views", "mail.tmpl")} {
		if !isWatchedFile(p) {
			t.Errorf("%s should trigger a rebuild", p)
		}
	}
	for _, p := range []string{".#main.go", filepath.Join("config", ".config.yaml.swp"), filepath.Join("routers", ".hidden.go")} {
		if isWatchedFile(p) {
			t.Errorf("%s should not trigger a rebuild", p)
		}
	}
}

// TestRunPassesArgsAndExitCode：-- 之後的參數傳給應用程式；--watch=false 以應用程式的結束碼結束。
// 先前 hyp run -- version 回 unknown command；應用程式 os.Exit(3) 時 hyp 一律以 1 結束
func TestRunPassesArgsAndExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module exitapp\n\ngo 1.24\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"os\"\n\nfunc main() { os.Exit(10 + len(os.Args[1:])) }\n"), 0644)
	t.Chdir(dir)

	// 沒有 -- 的位置參數仍然回錯（打錯的子命令不該變成應用程式的參數）。
	// 先測這個：pflag 記著上一次解析到的 -- 位置，同一個行程裡不會歸零
	var code exitCodeError
	err := runHyp(t, "run", "--watch=false", "serve")
	if err == nil || !strings.Contains(err.Error(), "--") || errors.As(err, &code) {
		t.Errorf("hyp run serve: got %v, want an error pointing at --", err)
	}

	err = runHyp(t, "run", "--watch=false", "--", "serve", "--port")
	if !errors.As(err, &code) || int(code) != 12 {
		t.Errorf("hyp run -- serve --port: got %v, want the app's exit status 12", err)
	}
}

// TestRunRejectsNonMainPackage：在非 main 套件的目錄執行時說明原因。
// 先前 go build -o 產出的不是執行檔，錯誤是作業系統回的「不是有效的應用程式」
func TestRunRejectsNonMainPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module libapp\n\ngo 1.24\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "routers"), 0755)
	os.WriteFile(filepath.Join(dir, "routers", "r.go"), []byte("package routers\n"), 0644)
	t.Chdir(filepath.Join(dir, "routers"))

	err := runDev(context.Background(), ".", false, nil)
	if err == nil || !strings.Contains(err.Error(), "not a main package") {
		t.Errorf("got %v, want an error saying this is not a main package", err)
	}
}
