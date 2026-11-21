// @chris
package logger

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/natefinch/lumberjack.v2"
)

// RotationConfig 日誌輪轉設定（對應 config.yaml 的 logger.rotation 區段）。
//
//   - MaxSize：單檔上限，接受 "100MB"、"100M"、"1.5GB"、"512KB" 或純位元組數；
//     底層以 MB 為單位，不足 1MB 的值一律進位為 1MB。空字串或 "0" 表示不依大小輪轉。
//   - MaxAge：備份的保留期限（如 "12h"、"7d"、"1w"），換算成天數（無條件進位、最少 1 天），
//     超過期限的備份在輪轉後刪除。空字串或 "0" 表示不依時間刪除。
//     先前 MaxAge 是「自行程啟動起每隔多久輪轉一次」，從未刪除舊備份。
//   - MaxBackups：最多保留幾份備份（0 = 不限）。
//   - Compress：備份是否 gzip。
type RotationConfig struct {
	MaxSize    string
	MaxAge     string
	MaxBackups int
	Compress   bool
}

// defaultRotationConfig 與 pkg/config 的 ApplyDefaults 一致（New 以檔案路徑為 output 時使用）
func defaultRotationConfig() *RotationConfig {
	return &RotationConfig{MaxSize: "100MB", MaxAge: "7d", MaxBackups: 10}
}

// Rotation 依大小自動輪轉的檔案 writer，底層為 gopkg.in/natefinch/lumberjack.v2。
//
// 備份檔名為 <name>-<2006-01-02T15-04-05.000>.<ext>（本地時間），清理只認得這種
// 時間戳記檔名，旁邊的 app-access.log 之類檔案不受影響。
type Rotation struct {
	mu       sync.Mutex
	lj       *lumberjack.Logger
	size     int64 // 目前檔案大小（由本層追蹤，讓輪轉一律經過 rotateLocked）
	maxBytes int64 // 0 = 不依大小輪轉
	last     time.Time
	closed   bool

	blocked      bool      // 輪轉失敗中：直接附加到目前的檔案
	blockedSince time.Time // 開始失敗的時間
}

// ljRotate 供測試替換（模擬 rename 失敗）
var ljRotate = (*lumberjack.Logger).Rotate

// rotateRetry 輪轉失敗後多久再試一次（每次嘗試都要關檔、rename）；測試會縮短
var rotateRetry = time.Second

// noSizeLimitMB 代表「不依大小輪轉」（lumberjack 的 MaxSize 為 0 時會改用 100MB 預設值）
const noSizeLimitMB = math.MaxInt32

// NewRotation 建立寫入 filename 的輪轉 writer；設定值無效時回傳錯誤。
func NewRotation(filename string, config *RotationConfig) (*Rotation, error) {
	if config == nil {
		config = &RotationConfig{}
	}
	if config.MaxBackups < 0 {
		return nil, fmt.Errorf("invalid max backups: %d", config.MaxBackups)
	}

	sizeMB := noSizeLimitMB
	if config.MaxSize != "" {
		bytes, err := parseSize(config.MaxSize)
		if err != nil {
			return nil, fmt.Errorf("invalid max size: %w", err)
		}
		if bytes > 0 {
			sizeMB = int(min((bytes+megabyte-1)/megabyte, noSizeLimitMB))
		}
	}

	ageDays := 0
	if config.MaxAge != "" {
		age, err := parseAge(config.MaxAge)
		if err != nil {
			return nil, fmt.Errorf("invalid max age: %w", err)
		}
		if age > 0 {
			ageDays = int(min((age+24*time.Hour-1)/(24*time.Hour), math.MaxInt32))
		}
	}

	// 確保日誌目錄存在，並預先以 0644 建檔：lumberjack 新建檔預設 0600，
	// 輪轉時則沿用既有檔案的權限
	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, err
	}

	r := &Rotation{
		lj: &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    sizeMB,
			MaxAge:     ageDays,
			MaxBackups: config.MaxBackups,
			LocalTime:  true,
			Compress:   config.Compress,
		},
		size: info.Size(),
	}
	if sizeMB != noSizeLimitMB {
		r.maxBytes = int64(sizeMB) * megabyte
	}
	return r, nil
}

const megabyte = 1024 * 1024

// parseSize 解析大小字串為位元組數：接受 KB/K、MB/M、GB/G、B 後綴（不分大小寫、1024 進位）
// 與小數（"1.5GB"），拒絕負數與無法解析的值。
// 先前只認 KB/MB/GB 且只收整數，"100M"、"1.5GB" 被拒、"-5MB" 反而通過。
func parseSize(s string) (int64, error) {
	orig := s
	s = strings.ToUpper(strings.TrimSpace(s))
	multiplier := float64(1)
	for _, u := range []struct {
		suffix string
		mult   float64
	}{
		{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"B", 1},
	} {
		if strings.HasSuffix(s, u.suffix) {
			multiplier = u.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}

	// 只接受十進位數字與至多一個小數點（ParseFloat 另外接受 "Inf"、"1e3"、"0x1p3"）
	if s == "" || s == "." || strings.Count(s, ".") > 1 || strings.Trim(s, "0123456789.") != "" {
		return 0, fmt.Errorf("invalid size %q", orig)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", orig)
	}
	bytes := math.Ceil(v * multiplier)
	if bytes > math.MaxInt64/2 {
		return 0, fmt.Errorf("size %q too large", orig)
	}
	return int64(bytes), nil
}

// parseAge 解析時間長度：h / d / w 後綴或 time.ParseDuration 格式，拒絕負數。
// 大寫 M 回錯：先前轉成小寫後當成分鐘，"1M"（想寫一個月）變成保留 1 天
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "M") {
		return 0, fmt.Errorf("%q: M is ambiguous (month or minute): write days or weeks, e.g. 30d or 4w", s)
	}
	s = strings.ToLower(s)

	var d time.Duration
	var err error
	unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if n := len(s); n > 1 && unit[s[n-1]] != 0 && strings.Trim(s[:n-1], "0123456789") == "" {
		var v int
		v, err = strconv.Atoi(s[:n-1])
		d = time.Duration(v) * unit[s[n-1]]
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("negative age %q", s)
	}
	return d, nil
}

// Write 寫入日誌，寫入前若會超過 max_size 則先輪轉。
//
// 單行超過 max_size 時截斷到上限（截在完整字元的邊界、保留結尾換行），不會為它切出空的備份檔；
// 先前會先輪轉出一個空備份再寫入。
//
// 輪轉失敗（例如 Windows 上有其他程式開著檔案導致 rename 失敗）時照樣寫進目前的檔案，
// 檔案暫時超過 max_size，之後每隔 rotateRetry 再試一次，並在 stderr 各印一次失敗與恢復。
// 先前失敗的那一筆直接丟掉：只要檔案還被別人開著，之後每一筆日誌都消失。
func (r *Rotation) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, os.ErrClosed
	}

	// 截斷到 max_size-1：lumberjack 首次開檔時以「既有大小+本筆 >= 上限」決定輪轉，
	// 剛好等於上限的空檔也會被切成空備份
	n := len(p)
	if limit := r.maxBytes - 1; r.maxBytes > 0 && int64(len(p)) > limit {
		cut := int(limit - 1)
		for cut > 0 && !utf8.RuneStart(p[cut]) { // 不截在多位元組字元中間
			cut--
		}
		trunc := make([]byte, cut+1)
		copy(trunc, p[:cut])
		trunc[cut] = '\n'
		p = trunc
	}

	switch {
	case r.blocked:
		// 失敗中：不論本層記錄的大小，定期再試（被別的行程寫大的檔案，本層的 size 並不知道）
		if time.Since(r.last) >= rotateRetry {
			r.noteRotate(r.rotateLocked())
		}
	case r.maxBytes > 0 && r.size > 0 && r.size+int64(len(p)) >= r.maxBytes:
		r.noteRotate(r.rotateLocked())
	}

	var written int
	var err error
	if !r.blocked {
		written, err = r.lj.Write(p)
		// 檔案被別的行程寫大時，lumberjack 會在 Write 裡自行輪轉；它失敗時同樣改為直接附加。
		// 先前這條路徑每一筆都回錯：兩個行程寫同一個檔時，其中一個的日誌全部消失
		if err != nil && written == 0 && r.maxBytes > 0 {
			r.last = time.Now()
			r.noteRotate(err)
		}
	}
	if r.blocked {
		written, err = r.appendDirect(p)
	}
	r.size += int64(written)
	if err != nil {
		return written, err
	}
	return n, nil
}

// noteRotate 記錄輪轉的結果，狀態改變時在 stderr 印一行
func (r *Rotation) noteRotate(err error) {
	switch {
	case err != nil && !r.blocked:
		r.blocked, r.blockedSince = true, time.Now()
		fmt.Fprintf(errOut, "logger: cannot rotate %s, still writing to it and retrying: %v\n", r.lj.Filename, err)
	case err == nil && r.blocked:
		r.blocked = false
		fmt.Fprintf(errOut, "logger: %s rotated after failing for %s\n", r.lj.Filename, time.Since(r.blockedSince).Round(time.Second))
	}
}

// appendDirect 繞過 lumberjack 直接附加：它在檔案超過上限時只會再試輪轉，不肯寫入。
// 每次都開檔、關檔：Windows 上自己持有的控制代碼也會讓 rename 失敗
func (r *Rotation) appendDirect(p []byte) (int, error) {
	f, err := os.OpenFile(r.lj.Filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, err
	}
	n, err := f.Write(p)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// Rotate 立即強制輪轉（無視門檻），供外部（如訊號 handler）強制切檔。
func (r *Rotation) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return os.ErrClosed
	}
	err := r.rotateLocked()
	if err == nil {
		r.noteRotate(nil)
	}
	return err
}

// rotateLocked 執行輪轉（呼叫方須持有鎖）。
// 備份檔名精確到毫秒，同一毫秒內再次輪轉時先等到下一毫秒，
// 否則 rename 會覆蓋上一份備份（lumberjack 本身不處理這種碰撞）。
func (r *Rotation) rotateLocked() error {
	if !r.last.IsZero() {
		next := r.last.Truncate(time.Millisecond).Add(time.Millisecond)
		for now := time.Now(); now.Before(next); now = time.Now() {
			time.Sleep(next.Sub(now))
		}
	}
	err := ljRotate(r.lj)
	r.last = time.Now()
	if err != nil {
		return err
	}
	r.size = 0
	return nil
}

// Close 關閉目前的日誌檔；之後的 Write / Rotate 回傳 os.ErrClosed，不會重新開檔。
// 重複呼叫安全。
func (r *Rotation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if err := r.lj.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}
