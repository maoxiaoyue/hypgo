// @chris
package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RotationConfig struct {
	MaxSize    string
	MaxAge     string
	MaxBackups int
	Compress   bool
}

type Rotation struct {
	mu         sync.Mutex
	filename   string
	file       *os.File
	size       int64
	maxSize    int64
	maxAge     time.Duration
	maxBackups int
	compress   bool
	startTime  time.Time
}

func NewRotation(filename string, config *RotationConfig) (*Rotation, error) {
	r := &Rotation{
		filename:   filename,
		maxBackups: config.MaxBackups,
		compress:   config.Compress,
	}

	// 解析最大檔案大小
	if config.MaxSize != "" {
		size, err := parseSize(config.MaxSize)
		if err != nil {
			return nil, fmt.Errorf("invalid max size: %w", err)
		}
		r.maxSize = size
	}

	// 解析最大保留時間
	if config.MaxAge != "" {
		age, err := parseAge(config.MaxAge)
		if err != nil {
			return nil, fmt.Errorf("invalid max age: %w", err)
		}
		r.maxAge = age
	}

	// 確保日誌目錄存在
	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
	}

	if err := r.openFile(); err != nil {
		return nil, err
	}

	return r, nil
}

func parseSize(s string) (int64, error) {
	s = strings.ToUpper(s)
	multiplier := int64(1)

	if strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = s[:len(s)-2]
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = s[:len(s)-2]
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-2]
	}

	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}

	return value * multiplier, nil
}

func parseAge(s string) (time.Duration, error) {
	s = strings.ToLower(s)

	if strings.HasSuffix(s, "h") {
		hours, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(hours) * time.Hour, nil
	} else if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	} else if strings.HasSuffix(s, "w") {
		weeks, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}

// Write 寫入日誌並在達到門檻時自動輪轉（lumberjack 同款行為：
// 輪轉在寫入路徑內觸發，無需外部呼叫 Rotate）
func (r *Rotation) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		if err := r.openFile(); err != nil {
			return 0, err
		}
	}

	// 寫入前檢查門檻；達標先輪轉再寫
	if r.shouldRotate(int64(len(p))) {
		if err := r.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err = r.file.Write(p)
	r.size += int64(n)

	return n, err
}

// shouldRotate 判斷是否達到輪轉門檻（呼叫方須持有鎖）。
// 年齡以「本行程開啟此檔的時間」起算（startTime），不用 ModTime——
// ModTime 每次寫入都會刷新，持續寫入的檔案會永不過期
func (r *Rotation) shouldRotate(incoming int64) bool {
	if r.maxSize > 0 && r.size+incoming > r.maxSize {
		return true
	}
	if r.maxAge > 0 && time.Since(r.startTime) >= r.maxAge {
		return true
	}
	return false
}

// Rotate 立即強制輪轉（無視門檻）。
// Write 已依 max_size / max_age 自動輪轉；此方法供外部（如訊號 handler）強制切檔
func (r *Rotation) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rotateLocked()
}

// rotateLocked 執行輪轉（呼叫方須持有鎖）
func (r *Rotation) rotateLocked() error {
	// 關閉當前檔案
	if err := r.file.Close(); err != nil {
		return err
	}

	// 重命名當前檔案
	newName := r.backupName()
	if err := os.Rename(r.filename, newName); err != nil {
		return err
	}

	// 壓縮檔案
	if r.compress {
		go r.compressFile(newName)
	}

	// 清理舊檔案
	if r.maxBackups > 0 {
		go r.cleanupOldFiles()
	}

	// 開啟新檔案
	return r.openFile()
}

func (r *Rotation) openFile() error {
	file, err := os.OpenFile(r.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	r.file = file
	r.size = 0
	r.startTime = time.Now()

	// 獲取檔案大小
	info, err := file.Stat()
	if err == nil {
		r.size = info.Size()
	}

	return nil
}

func (r *Rotation) backupName() string {
	dir := filepath.Dir(r.filename)
	filename := filepath.Base(r.filename)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]

	// 毫秒精度：避免同一秒內連續輪轉時備份檔名衝突（rename 失敗）
	timestamp := time.Now().Format("20060102-150405.000")
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, timestamp, ext))
}

func (r *Rotation) compressFile(filename string) {
	source, err := os.Open(filename)
	if err != nil {
		return
	}
	defer source.Close()

	target, err := os.Create(filename + ".gz")
	if err != nil {
		return
	}
	defer target.Close()

	gz := gzip.NewWriter(target)
	defer gz.Close()

	if _, err := io.Copy(gz, source); err != nil {
		return
	}

	os.Remove(filename)
}

func (r *Rotation) cleanupOldFiles() {
	dir := filepath.Dir(r.filename)
	prefix := filepath.Base(r.filename)
	ext := filepath.Ext(prefix)
	prefix = prefix[:len(prefix)-len(ext)]

	files, err := filepath.Glob(filepath.Join(dir, prefix+"-*"))
	if err != nil {
		return
	}

	if len(files) <= r.maxBackups {
		return
	}

	sort.Strings(files)

	for i := 0; i < len(files)-r.maxBackups; i++ {
		os.Remove(files[i])
	}
}

func (r *Rotation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}
