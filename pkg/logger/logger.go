package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
type Logger struct {
	level    Level
	logger   *log.Logger
	file     *os.File
	mu       sync.Mutex
	rotator  *LogRotator
	colorize bool
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
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput 設定輸出
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.SetOutput(w)
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
	l.logger.SetOutput(file)

	return nil
}

// formatMessage 格式化訊息
func (l *Logger) formatMessage(level Level, msg string, keysAndValues ...interface{}) string {
	levelStr := l.getLevelString(level)
	color := l.getLevelColor(level)

	// 格式化額外的鍵值對
	extra := ""
	if len(keysAndValues) > 0 {
		extra = " |"
		for i := 0; i < len(keysAndValues); i += 2 {
			if i+1 < len(keysAndValues) {
				extra += fmt.Sprintf(" %v=%v", keysAndValues[i], keysAndValues[i+1])
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

// log 通用日誌方法
func (l *Logger) log(level Level, msg string, keysAndValues ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	formattedMsg := l.formatMessage(level, msg, keysAndValues...)
	l.logger.Println(formattedMsg)

	// 檢查是否需要輪轉
	if l.rotator != nil {
		l.checkRotation()
	}
}

// Debug 輸出調試日誌
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.log(DEBUG, msg, keysAndValues...)
}

// Info 輸出信息日誌
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.log(INFO, msg, keysAndValues...)
}

// Notice 輸出通知日誌
func (l *Logger) Notice(msg string, keysAndValues ...interface{}) {
	l.log(NOTICE, msg, keysAndValues...)
}

// Warn 輸出警告日誌
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.log(WARNING, msg, keysAndValues...)
}

// Warning 輸出警告日誌（別名）
func (l *Logger) Warning(msg string, keysAndValues ...interface{}) {
	l.log(WARNING, msg, keysAndValues...)
}

// Error 輸出錯誤日誌
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.log(ERROR, msg, keysAndValues...)
}

// Emergency 輸出緊急日誌
func (l *Logger) Emergency(msg string, keysAndValues ...interface{}) {
	l.log(EMERGENCY, msg, keysAndValues...)
}

// Fatal 輸出致命錯誤並退出
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.log(EMERGENCY, msg, keysAndValues...)
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
		// 按時間排序（最舊的在前）
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
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}
