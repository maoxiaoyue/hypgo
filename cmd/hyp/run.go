// @chris
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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

// runSkipDirs 不監看的目錄（以 . 開頭的隱藏目錄一律略過）
var runSkipDirs = map[string]bool{
	"vendor": true, "logs": true, "tmp": true, "node_modules": true, "testdata": true,
}

func init() {
	runCmd.Flags().BoolVarP(&watch, "watch", "w", true, "Rebuild and restart on .go / .yaml changes (--watch=false to disable)")
	runCmd.Flags().StringVarP(&runPkg, "pkg", "p", ".", "Main package to build")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the HypGo application with hot reload",
	Long: `Build the project (go build into a temp directory) and run the binary.

With --watch (default) the project tree is watched: any change to a .go file
(except *_test.go), a .yaml / .yml file or go.mod rebuilds the binary, stops the
running process and starts the new one. .git, vendor, logs, tmp, testdata,
node_modules and hidden directories (.hyp, .idea, ...) are not watched.
A failed build keeps the previous process running.

The running process is stopped with SIGTERM on Unix (graceful shutdown, Kill
after 10s) and Process.Kill on Windows, so the port is released before the new
process starts.

On startup, AutoSync writes .hyp/context.yaml with the current project manifest.

Examples:
  hyp run                    Build, run and hot reload
  hyp run --watch=false      Build and run once
  hyp run -p ./cmd/server    Main package outside the project root`,
	RunE:         runRun,
	SilenceUsage: true,
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDev(ctx, runPkg, watch)
}

// runDev 以 go build 產出的執行檔啟動應用；watch 為 true 時於檔案變更後重建並重啟，直到 ctx 結束。
// 先前以 go run 啟動並送 SIGTERM：Windows 不支援送該訊號（cmd.Wait 永遠等不到），
// Unix 上只殺到 go run 這層包裝，真正的子行程繼續占著埠，新行程因此起不來
func runDev(ctx context.Context, pkg string, watch bool) error {
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
			return err
		}
		p, err := startApp(bin)
		if err != nil {
			return err
		}
		select {
		case <-p.done:
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
	if err := addWatchDirs(w, "."); err != nil {
		return err
	}
	fmt.Println("🔥 Hot reload enabled (watching .go / .yaml files)")

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
		if proc, err = startApp(bin); err != nil {
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
			proc = nil
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() && !skipWatchDir(fi.Name()) {
					addWatchDirs(w, event.Name)
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 || !isWatchedFile(event.Name) {
				continue
			}
			changed = event.Name
			debounce.Reset(runDebounce)
		case <-debounce.C:
			fmt.Printf("🔄 Reloading due to change in: %s\n", changed)
			restart()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "Watch error: %v\n", err)
		}
	}
}

// devProcess 一個執行中的應用程式行程；done 在行程結束後關閉，err 為其結束狀態
type devProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func startApp(bin string) (*devProcess, error) {
	cmd := exec.Command(bin)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &devProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// stop 請行程結束並等它退出：Unix 送 SIGTERM（server 會優雅關閉），Windows 無法送該訊號，直接 Kill；
// 逾時仍未退出則 Kill
func (p *devProcess) stop(timeout time.Duration) {
	select {
	case <-p.done:
		return
	default:
	}
	if runtime.GOOS == "windows" {
		p.cmd.Process.Kill()
	} else {
		p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.done:
	case <-time.After(timeout):
		p.cmd.Process.Kill()
		<-p.done
	}
}

// addWatchDirs 遞迴監看 root 底下的目錄（略過 skipWatchDir）
func addWatchDirs(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 讀不到的目錄略過，不中斷整個監看
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipWatchDir(d.Name()) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

// skipWatchDir 判斷目錄是否不需監看（隱藏目錄如 .git / .hyp，以及 vendor、logs 等）
func skipWatchDir(name string) bool {
	return (strings.HasPrefix(name, ".") && name != ".") || runSkipDirs[name]
}

// isWatchedFile 判斷檔案變更是否需要重建：.go（測試檔除外）、.yaml / .yml、go.mod
func isWatchedFile(path string) bool {
	name := filepath.Base(path)
	switch filepath.Ext(name) {
	case ".go":
		return !strings.HasSuffix(name, "_test.go")
	case ".yaml", ".yml":
		return true
	}
	return name == "go.mod"
}
