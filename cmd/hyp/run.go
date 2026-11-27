// @chris
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var (
	watch  bool
	runPkg string
)

// runDebounce 合併短時間內的連續檔案事件（編輯器存檔常一次觸發多個 Write / Create）
const runDebounce = 300 * time.Millisecond

// runStopTimeout 送出結束訊號後等待子行程退出的上限，逾時改 Kill
const runStopTimeout = 10 * time.Second

// runPIDFile server.Start() 寫在工作目錄的 PID 檔
const runPIDFile = "hypgo.pid"

// runSkipDirs 不監看的目錄（以 . 開頭的隱藏目錄一律略過）
var runSkipDirs = map[string]bool{
	"vendor": true, "logs": true, "tmp": true, "node_modules": true, "testdata": true,
}

// ttyInterrupt hyp run 是被 SIGINT 結束的。Unix 的終端機把 Ctrl+C 送給整個前景行程群組，
// 應用程式已經收到一次，不能再補一個訊號（見 run_other.go 的 interruptChild）
var ttyInterrupt atomic.Bool

// exitCodeError 應用程式自己的結束碼：hyp run --watch=false 以同一個碼結束
type exitCodeError int

func (e exitCodeError) Error() string { return "exit status " + strconv.Itoa(int(e)) }

func init() {
	runCmd.Flags().BoolVarP(&watch, "watch", "w", true, "Rebuild and restart on .go / .yaml / .html changes (--watch=false to disable)")
	runCmd.Flags().StringVarP(&runPkg, "pkg", "p", ".", "Main package to build")
}

var runCmd = &cobra.Command{
	Use:   "run [flags] [-- app-args...]",
	Short: "Run the HypGo application with hot reload",
	Long: `Build the project (go build into a temp directory) and run the binary.

With --watch (default) the project tree is watched: any change to a .go file
(except *_test.go), a .yaml / .yml file, a .html / .tmpl file or go.mod rebuilds
the binary, stops the running process and starts the new one. .git, vendor,
logs, tmp, testdata, node_modules, hidden directories (.hyp, .idea, ...) and
hidden files are not watched. A failed build keeps the previous process running.

The running process is asked to stop (SIGTERM on Unix, Ctrl+Break on Windows)
so the server shuts down gracefully, and is killed after 10s. The new process
starts once the old one has exited and released the port.

Everything after -- is passed to the application. With --watch=false, hyp run
exits with the application's exit code.

On startup, AutoSync writes .hyp/context.yaml with the current project manifest.

Examples:
  hyp run                    Build, run and hot reload
  hyp run --watch=false      Build and run once
  hyp run -p ./cmd/server    Main package outside the project root
  hyp run -- serve --port 9  Pass arguments to the application`,
	// 沒有 -- 的位置參數一律回錯（打錯的子命令不會被當成應用程式的參數）
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 && cmd.ArgsLenAtDash() != 0 {
			return fmt.Errorf("unexpected argument %q: arguments for the application go after --, as in: hyp run -- %s", args[0], strings.Join(args, " "))
		}
		return nil
	},
	RunE:         runRun,
	SilenceUsage: true,
}

func runRun(cmd *cobra.Command, args []string) error {
	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigc)
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	go func() {
		select {
		case s := <-sigc:
			ttyInterrupt.Store(s == os.Interrupt)
			cancel()
		case <-ctx.Done():
		}
	}()

	err := runDev(ctx, runPkg, watch, args)
	// 應用程式自己的結束碼不是 hyp 的錯誤：不印 Error:，由 main 以同一個碼結束
	var code exitCodeError
	if errors.As(err, &code) {
		cmd.SilenceErrors = true
	}
	return err
}

// checkMainPackage 確認 pkg 是 main 套件。先前在 routers/ 之類的子目錄執行時，go build -o
// 對非 main 套件產出的不是執行檔，錯誤是啟動它時作業系統回的「不是有效的應用程式」。
// go list 本身失敗時（不在 module 內、編譯錯誤）交給接下來的 go build 回報
func checkMainPackage(ctx context.Context, pkg string) error {
	out, err := exec.CommandContext(ctx, "go", "list", "-f", "{{.Name}}", pkg).Output()
	if err != nil {
		return nil
	}
	if name := strings.TrimSpace(string(out)); name != "" && name != "main" {
		return fmt.Errorf("%s is package %s, not a main package: run hyp run from the directory of main.go (usually the project root) or pass -p <main package>", pkg, name)
	}
	return nil
}

// runDev 以 go build 產出的執行檔啟動應用；watch 為 true 時於檔案變更後重建並重啟，直到 ctx 結束。
// appArgs 原樣傳給應用程式。
// 先前以 go run 啟動並送 SIGTERM：Windows 不支援送該訊號（cmd.Wait 永遠等不到），
// Unix 上只殺到 go run 這層包裝，真正的子行程繼續占著埠，新行程因此起不來
func runDev(ctx context.Context, pkg string, watch bool, appArgs []string) error {
	if err := checkMainPackage(ctx, pkg); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "hyp-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	builds := 0
	build := func() (string, error) {
		builds++
		bin := filepath.Join(tmpDir, fmt.Sprintf("app-%d", builds))
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		b := exec.CommandContext(ctx, "go", "build", "-o", bin, pkg)
		b.Stdout, b.Stderr = os.Stderr, os.Stderr
		if err := b.Run(); err != nil {
			return "", fmt.Errorf("go build %s: %w", pkg, err)
		}
		return bin, nil
	}

	if !watch {
		bin, err := build()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		p, err := startApp(bin, appArgs)
		if err != nil {
			return err
		}
		select {
		case <-p.done:
			removeStalePID(p.cmd.Process.Pid)
			// 先前應用程式 os.Exit(3) 時印出 Error: exit status 3 並以 1 結束
			var ee *exec.ExitError
			if errors.As(p.err, &ee) && ee.ExitCode() > 0 {
				return exitCodeError(ee.ExitCode())
			}
			return p.err
		case <-ctx.Done():
			p.stop(runStopTimeout)
			return nil
		}
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	// 事件由這個 goroutine 一路讀走，主迴圈只收「有檔案變了」。
	// 先前主迴圈自己讀 w.Events，又在迴圈裡呼叫 w.Add：Windows 的 fsnotify 要靠讀取事件的
	// goroutine 處理 Add，而它正等著把下一個事件交給沒在讀的主迴圈，兩邊互等——
	// 一次建立多層目錄（mkdir -p、hyp generate）之後熱重載就永久停住，沒有任何訊息
	changes := make(chan string, 1)
	notify := func(name string) {
		select {
		case changes <- name:
		default: // 已經有一筆待處理的變更，重建一次就夠
		}
	}
	go func() {
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
						if !skipWatchDir(fi.Name()) {
							// 另開 goroutine：這裡不能等 w.Add
							go func(dir string) {
								// 目錄的 Create 事件到之前就寫進去的檔案沒有事件，在這裡補上
								if found, _ := addWatchDirs(w, dir); found != "" {
									notify(found)
								}
							}(event.Name)
						}
						continue
					}
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 && isWatchedFile(event.Name) {
					notify(event.Name)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "Watch error: %v\n", err)
			}
		}
	}()
	if _, err := addWatchDirs(w, "."); err != nil {
		return err
	}
	fmt.Println("🔥 Hot reload enabled (watching .go / .yaml / .html files)")

	var proc *devProcess
	var bin string
	defer func() {
		if proc != nil {
			proc.stop(runStopTimeout)
		}
	}()

	restart := func() {
		newBin, err := build()
		if err != nil {
			if ctx.Err() != nil {
				return // Ctrl+C 落在編譯途中：不是編譯錯誤
			}
			if proc != nil {
				fmt.Fprintf(os.Stderr, "❌ %v (previous process keeps running)\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "❌ %v (waiting for changes)\n", err)
			}
			return
		}
		if proc != nil {
			proc.stop(runStopTimeout)
			proc = nil
			os.Remove(bin)
		}
		bin = newBin
		if proc, err = startApp(bin, appArgs); err != nil {
			fmt.Fprintf(os.Stderr, "❌ start app: %v\n", err)
		}
	}
	restart()

	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	changed := ""
	for {
		var exited <-chan struct{}
		if proc != nil {
			exited = proc.done
		}
		select {
		case <-ctx.Done():
			return nil
		case <-exited:
			if proc.err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  app exited: %v (waiting for changes)\n", proc.err)
			} else {
				fmt.Println("⚠️  app exited (waiting for changes)")
			}
			removeStalePID(proc.cmd.Process.Pid)
			proc = nil
		case changed = <-changes:
			debounce.Reset(runDebounce)
		case <-debounce.C:
			fmt.Printf("🔄 Reloading due to change in: %s\n", changed)
			restart()
		}
	}
}

// devProcess 一個執行中的應用程式行程；done 在行程結束後關閉，err 為其結束狀態
type devProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func startApp(bin string, args []string) (*devProcess, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	prepareChild(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	bindChildLifetime(cmd)
	p := &devProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// stop 請行程結束並等它退出：Unix 送 SIGTERM、Windows 送 Ctrl+Break（server 都會優雅關閉），
// 送不出去或逾時仍未退出則 Kill。
// 先前 Windows 一律直接 Kill：OnShutdown、main 裡的 defer、進行中的請求都被跳過
func (p *devProcess) stop(timeout time.Duration) {
	defer removeStalePID(p.cmd.Process.Pid)
	select {
	case <-p.done:
		return
	default:
	}
	if err := interruptChild(p.cmd); err != nil {
		p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
	case <-time.After(timeout):
		p.cmd.Process.Kill()
		<-p.done
	}
}

// removeStalePID 應用程式沒能自己清掉 PID 檔時（被 Kill、當掉）代為移除；只移除記著該行程的那一份
func removeStalePID(pid int) {
	data, err := os.ReadFile(runPIDFile)
	if err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(pid) {
		os.Remove(runPIDFile)
	}
}

// addWatchDirs 遞迴監看 root 底下的目錄（略過 skipWatchDir）；found 是途中看到的一個會觸發重建的檔案
func addWatchDirs(w *fsnotify.Watcher, root string) (found string, err error) {
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 讀不到的目錄略過，不中斷整個監看
		}
		if !d.IsDir() {
			if isWatchedFile(path) {
				found = path
			}
			return nil
		}
		if path != root && skipWatchDir(d.Name()) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
	return found, err
}

// skipWatchDir 判斷目錄是否不需監看（隱藏目錄如 .git / .hyp，以及 vendor、logs 等）
func skipWatchDir(name string) bool {
	return (strings.HasPrefix(name, ".") && name != ".") || runSkipDirs[name]
}

// isWatchedFile 判斷檔案變更是否需要重建：.go（測試檔除外）、.yaml / .yml、go.mod，
// 以及 .html / .tmpl（web 模板以 //go:embed 編進執行檔，先前改了樣板不會重載）。
// 點開頭的檔案不算：Emacs 的鎖檔 .#main.go 每次開始編輯都會出現
func isWatchedFile(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch filepath.Ext(name) {
	case ".go":
		return !strings.HasSuffix(name, "_test.go")
	case ".yaml", ".yml", ".html", ".tmpl":
		return true
	}
	return name == "go.mod"
}
