// @chris
package logger

import (
	stdcontext "context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level 日誌級別
type Level int

const (
	DEBUG Level = iota
	INFO
	NOTICE
	WARNING
	ERROR
	EMERGENCY
)

// Logger 日誌介面
//
// 提供兩組互補的輸出方法，呼叫端依語意明確選擇，框架不再以啟發式猜測：
//   - KV 模式：Info/Debug/...（msg 為純文字，後接成對 key、value）
//   - printf 模式：Infof/Debugf/...（msg 含 %v/%s/%d 等格式動詞）
//
// 文字（含彩色）輸出走 log.Logger 後端；json 模式改由 log/slog 的 JSONHandler
// 輸出標準結構化日誌，可直接餵 Loki / Datadog / CloudWatch。
type Logger struct {
	level    Level
	logger   *log.Logger  // 文字／彩色輸出後端（預設）
	slog     *slog.Logger // 結構化 JSON 後端（json 模式，由 log/slog 驅動）
	out      io.Writer    // 目前輸出目的地（slog 與輪轉檢查共用）
	json     bool         // true 時改用 slog JSON handler 輸出
	file     *os.File
	mu       sync.Mutex
	rotator  *LogRotator
	colorize bool
}

// New 建立 Logger（向後相容的建構子）。
// 永遠回傳可用實例（不會回傳 nil），即使建構過程無誤也已套用安全預設。
func New(level string, output string, writer io.Writer, colorEnabled bool) (*Logger, error) {
	var w io.Writer
	if writer != nil {
		w = writer
	} else if output == "stdout" {
		w = os.Stdout
	} else {
		w = os.Stderr
	}

	return &Logger{
		logger:   log.New(w, "", log.LstdFlags),
		out:      w,
		level:    parseLevel(level),
		colorize: colorEnabled,
	}, nil
}

// parseLevel 將字串轉換為 Level
func parseLevel(level string) Level {
	switch level {
	case "debug", "DEBUG":
		return DEBUG
	case "info", "INFO":
		return INFO
	case "notice", "NOTICE":
		return NOTICE
	case "warning", "WARNING", "warn", "WARN":
		return WARNING
	case "error", "ERROR":
		return ERROR
	case "emergency", "EMERGENCY":
		return EMERGENCY
	default:
		return INFO
	}
}

// LogRotator 日誌輪轉器
type LogRotator struct {
	filename   string
	maxSize    int64         // 最大文件大小（bytes）
	maxAge     int           // 最大保存天數
	maxBackups int           // 最大備份數量
	interval   time.Duration // 輪轉間隔
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Color codes for terminal output
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
)

// NewLogger 建立新的Logger實例
func NewLogger() *Logger {
	return &Logger{
		level:    INFO,
		logger:   log.New(os.Stdout, "", log.LstdFlags),
		out:      os.Stdout,
		colorize: true,
	}
}

// GetLogger 獲取全局Logger實例
func GetLogger() *Logger {
	once.Do(func() {
		defaultLogger = NewLogger()
	})
	return defaultLogger
}

// InitLogger 初始化Logger
func InitLogger(config interface{}) {
	// 這裡可以根據config初始化logger
	defaultLogger = NewLogger()
}

// SetLevel 設定日誌級別
func (l *Logger) SetLevel(level Level) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput 設定輸出
func (l *Logger) SetOutput(w io.Writer) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = w
	if l.logger != nil {
		l.logger.SetOutput(w)
	}
	// slog 透過 managedWriter 讀取 l.out，自動跟隨新輸出
}

// SetFormat 設定輸出格式："json" 啟用 log/slog 的結構化輸出，其餘維持文字模式。
// json 模式適合生產環境接 Loki / Datadog 等日誌聚合器。
func (l *Logger) SetFormat(format string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.json = strings.EqualFold(format, "json")
	if l.json && l.slog == nil {
		l.buildSlog()
	}
}

// buildSlog 以目前輸出建立 JSON slog logger。
// 層級過濾已在 log()/logf() 統一處理，故 handler 一律放行（LevelDebug）。
func (l *Logger) buildSlog() {
	h := slog.NewJSONHandler(managedWriter{l}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	l.slog = slog.New(h)
}

// managedWriter 讓 slog 寫入永遠跟隨 l.out，並在寫入後觸發輪轉檢查。
type managedWriter struct{ l *Logger }

func (w managedWriter) Write(p []byte) (int, error) {
	if w.l.out == nil {
		return len(p), nil
	}
	return w.l.out.Write(p)
}

// SetFile 設定日誌文件
func (l *Logger) SetFile(filename string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 關閉舊文件
	if l.file != nil {
		l.file.Close()
	}

	// 建立目錄
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 打開新文件
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	l.file = file
	l.out = file
	l.logger.SetOutput(file)

	return nil
}

// toSlog 將框架 Level 映射為 slog.Level（NOTICE/EMERGENCY 為自訂層級）
func (level Level) toSlog() slog.Level {
	switch level {
	case DEBUG:
		return slog.LevelDebug
	case INFO:
		return slog.LevelInfo
	case NOTICE:
		return slog.LevelInfo + 2
	case WARNING:
		return slog.LevelWarn
	case ERROR:
		return slog.LevelError
	case EMERGENCY:
		return slog.LevelError + 4
	default:
		return slog.LevelInfo
	}
}

// kvToAttrs 將成對的 key、value 轉為 slog.Attr（奇數時最後一個標記 MISSING）
func kvToAttrs(keysAndValues []interface{}) []slog.Attr {
	if len(keysAndValues) == 0 {
		return nil
	}
	attrs := make([]slog.Attr, 0, len(keysAndValues)/2+1)
	for i := 0; i < len(keysAndValues); i += 2 {
		key := fmt.Sprint(keysAndValues[i])
		if i+1 < len(keysAndValues) {
			attrs = append(attrs, slog.Any(key, keysAndValues[i+1]))
		} else {
			attrs = append(attrs, slog.String(key, "(MISSING)"))
		}
	}
	return attrs
}

// formatMessage 將純文字訊息與成對 key=value 格式化為文字輸出（KV 模式）。
//
// 注意：本方法不再解讀 % 格式動詞。printf-style 輸出請改用 Infof/Errorf 等方法，
// 避免「訊息中含字面 %（如百分比、URL、regex）」被誤判為格式字串而輸出亂碼。
func (l *Logger) formatMessage(level Level, msg string, keysAndValues ...interface{}) string {
	levelStr := l.getLevelString(level)
	color := l.getLevelColor(level)

	// 格式化額外的鍵值對（結構化日誌模式）
	extra := ""
	if len(keysAndValues) > 0 {
		extra = " |"
		for i := 0; i < len(keysAndValues); i += 2 {
			if i+1 < len(keysAndValues) {
				extra += fmt.Sprintf(" %v=%v", keysAndValues[i], keysAndValues[i+1])
			} else {
				// 奇數個參數：最後一個 key 沒有對應的 value
				extra += fmt.Sprintf(" %v=(MISSING)", keysAndValues[i])
			}
		}
	}

	if l.colorize && l.file == nil {
		return fmt.Sprintf("%s[%s]%s %s%s", color, levelStr, ColorReset, msg, extra)
	}

	return fmt.Sprintf("[%s] %s%s", levelStr, msg, extra)
}

// getLevelString 獲取級別字串
func (l *Logger) getLevelString(level Level) string {
	switch level {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case NOTICE:
		return "NOTICE"
	case WARNING:
		return "WARNING"
	case ERROR:
		return "ERROR"
	case EMERGENCY:
		return "EMERGENCY"
	default:
		return "UNKNOWN"
	}
}

// getLevelColor 獲取級別顏色
func (l *Logger) getLevelColor(level Level) string {
	switch level {
	case DEBUG:
		return ColorCyan
	case INFO:
		return ColorGreen
	case NOTICE:
		return ColorBlue
	case WARNING:
		return ColorYellow
	case ERROR:
		return ColorRed
	case EMERGENCY:
		return ColorPurple
	default:
		return ColorWhite
	}
}

// log 結構化 KV 日誌的共用實作
func (l *Logger) log(level Level, msg string, keysAndValues ...interface{}) {
	if l == nil || (l.logger == nil && l.slog == nil) {
		return
	}
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.json {
		if l.slog == nil {
			l.buildSlog()
		}
		l.slog.LogAttrs(stdcontext.Background(), level.toSlog(), msg, kvToAttrs(keysAndValues)...)
	} else {
		l.logger.Println(l.formatMessage(level, msg, keysAndValues...))
	}

	// 檢查是否需要輪轉
	if l.rotator != nil {
		l.checkRotation()
	}
}

// logf printf-style 日誌的共用實作（msg 含格式動詞，args 為對應參數）
func (l *Logger) logf(level Level, format string, args ...interface{}) {
	if l == nil || (l.logger == nil && l.slog == nil) {
		return
	}
	if level < l.level {
		return
	}

	msg := fmt.Sprintf(format, args...)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.json {
		if l.slog == nil {
			l.buildSlog()
		}
		l.slog.LogAttrs(stdcontext.Background(), level.toSlog(), msg)
	} else {
		l.logger.Println(l.formatMessage(level, msg))
	}

	if l.rotator != nil {
		l.checkRotation()
	}
}

// ===== KV 模式（結構化日誌）：msg 為純文字，後接成對 key、value =====

// Debug 輸出調試日誌（KV 模式）
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.log(DEBUG, msg, keysAndValues...)
}

// Info 輸出信息日誌（KV 模式）
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.log(INFO, msg, keysAndValues...)
}

// Notice 輸出通知日誌（KV 模式）
func (l *Logger) Notice(msg string, keysAndValues ...interface{}) {
	l.log(NOTICE, msg, keysAndValues...)
}

// Warn 輸出警告日誌（KV 模式）
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.log(WARNING, msg, keysAndValues...)
}

// Warning 輸出警告日誌（KV 模式，別名）
func (l *Logger) Warning(msg string, keysAndValues ...interface{}) {
	l.log(WARNING, msg, keysAndValues...)
}

// Error 輸出錯誤日誌（KV 模式）
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.log(ERROR, msg, keysAndValues...)
}

// Emergency 輸出緊急日誌（KV 模式）
func (l *Logger) Emergency(msg string, keysAndValues ...interface{}) {
	l.log(EMERGENCY, msg, keysAndValues...)
}

// Fatal 輸出致命錯誤並退出（KV 模式）
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.log(EMERGENCY, msg, keysAndValues...)
	os.Exit(1)
}

// ===== printf 模式：format 含 %v/%s/%d 等格式動詞，args 為對應參數 =====

// Debugf 以 printf 格式輸出調試日誌
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// Infof 以 printf 格式輸出信息日誌
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// Noticef 以 printf 格式輸出通知日誌
func (l *Logger) Noticef(format string, args ...interface{}) {
	l.logf(NOTICE, format, args...)
}

// Warnf 以 printf 格式輸出警告日誌
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WARNING, format, args...)
}

// Warningf 以 printf 格式輸出警告日誌（別名）
func (l *Logger) Warningf(format string, args ...interface{}) {
	l.logf(WARNING, format, args...)
}

// Errorf 以 printf 格式輸出錯誤日誌
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// Emergencyf 以 printf 格式輸出緊急日誌
func (l *Logger) Emergencyf(format string, args ...interface{}) {
	l.logf(EMERGENCY, format, args...)
}

// Fatalf 以 printf 格式輸出致命錯誤並退出
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logf(EMERGENCY, format, args...)
	os.Exit(1)
}

// checkRotation 檢查是否需要輪轉
func (l *Logger) checkRotation() {
	if l.file == nil || l.rotator == nil {
		return
	}

	info, err := l.file.Stat()
	if err != nil {
		return
	}

	// 檢查文件大小
	if l.rotator.maxSize > 0 && info.Size() >= l.rotator.maxSize {
		l.rotate()
	}

	// 檢查文件年齡
	if l.rotator.maxAge > 0 {
		age := time.Since(info.ModTime()).Hours() / 24
		if int(age) >= l.rotator.maxAge {
			l.rotate()
		}
	}
}

// rotate 執行日誌輪轉
func (l *Logger) rotate() {
	if l.file == nil {
		return
	}

	// 關閉當前文件
	l.file.Close()

	// 重命名文件
	timestamp := time.Now().Format("20060102-150405")
	newName := fmt.Sprintf("%s.%s", l.rotator.filename, timestamp)
	os.Rename(l.rotator.filename, newName)

	// 打開新文件
	file, err := os.OpenFile(l.rotator.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return
	}

	l.file = file
	l.out = file
	l.logger.SetOutput(file)

	// 清理舊備份
	l.cleanOldBackups()
}

// cleanOldBackups 清理舊備份
func (l *Logger) cleanOldBackups() {
	if l.rotator.maxBackups <= 0 {
		return
	}

	dir := filepath.Dir(l.rotator.filename)
	base := filepath.Base(l.rotator.filename)

	files, err := filepath.Glob(filepath.Join(dir, base+".*"))
	if err != nil {
		return
	}

	// 如果備份數量超過限制，刪除最舊的
	if len(files) > l.rotator.maxBackups {
		// 按檔名排序（時間戳格式 20060102-150405 保證字典序 = 時間序）
		sort.Strings(files)
		for i := 0; i < len(files)-l.rotator.maxBackups; i++ {
			os.Remove(files[i])
		}
	}
}

// SetRotator 設定日誌輪轉器
func (l *Logger) SetRotator(rotator *LogRotator) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotator = rotator
}

// Close 關閉Logger
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}
