// Package watcher 提供結構化的檔案監控與變更摘要功能
// 支援 debounce 機制和安全檔案過濾
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config 配置 watcher 行為
type Config struct {
	// Dirs 要監控的目錄清單
	Dirs []string

	// IgnorePatterns 忽略的檔案/目錄模式
	IgnorePatterns []string

	// Debounce 防抖時間（預設 500ms）
	Debounce time.Duration

	// OnChange 變更回呼（收到 ChangeSummary）
	OnChange func(ChangeSummary)
}

// defaultIgnorePatterns 預設忽略的路徑（安全考量）
var defaultIgnorePatterns = []string{
	".git",
	".env",
	".hyp",
	"node_modules",
	"vendor",
	"__pycache__",
	".DS_Store",
	"*.swp",
	"*.swo",
	"*~",
}

// Watcher 結構化檔案監控器
type Watcher struct {
	config  Config
	watcher *fsnotify.Watcher
	done    chan struct{}
	mu      sync.Mutex
	pending map[string]fsnotify.Op // debounce 累積的事件
}

// New 建立新的 Watcher
func New(config Config) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if config.Debounce == 0 {
		config.Debounce = 500 * time.Millisecond
	}

	// 合併預設忽略模式
	config.IgnorePatterns = append(defaultIgnorePatterns, config.IgnorePatterns...)

	w := &Watcher{
		config:  config,
		watcher: fw,
		done:    make(chan struct{}),
		pending: make(map[string]fsnotify.Op),
	}

	// 遞迴加入目錄
	for _, dir := range config.Dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if w.shouldIgnore(path) {
					return filepath.SkipDir
				}
				return fw.Add(path)
			}
			return nil
		})
	}

	return w, nil
}

// Start 開始監控（阻塞）
func (w *Watcher) Start() {
	var timer *time.Timer

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if w.shouldIgnore(event.Name) {
				continue
			}

			w.mu.Lock()
			w.pending[event.Name] = event.Op
			w.mu.Unlock()

			// debounce：重設計時器
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.config.Debounce, func() {
				w.flush()
			})

		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}

		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

// Stop 停止監控
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}

// flush 發送累積的變更摘要
func (w *Watcher) flush() {
	w.mu.Lock()
	events := w.pending
	w.pending = make(map[string]fsnotify.Op)
	w.mu.Unlock()

	if len(events) == 0 {
		return
	}

	summary := BuildSummary(events)
	if w.config.OnChange != nil {
		w.config.OnChange(summary)
	}
}

// shouldIgnore 判斷路徑是否應被忽略
func (w *Watcher) shouldIgnore(path string) bool {
	base := filepath.Base(path)
	for _, pattern := range w.config.IgnorePatterns {
		// 目錄名比對
		if strings.HasPrefix(pattern, ".") && base == pattern {
			return true
		}
		// 萬用字元比對
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		// 路徑片段比對
		if strings.Contains(path, string(filepath.Separator)+pattern+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
