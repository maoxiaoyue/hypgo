// @chris
package logger

import (
	stdcontext "context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// NOTICE / EMERGENCY 在 slog 上的自訂層級（介於 INFO 與 WARN 之間、ERROR 之上）
const (
	slogNotice    = slog.LevelInfo + 2
	slogEmergency = slog.LevelError + 4
)

// Logger 日誌介面
//
// 提供兩組互補的輸出方法，呼叫端依語意明確選擇，框架不再以啟發式猜測：
//   - KV 模式：Info/Debug/...（msg 為純文字，後接成對 key、value）
//   - printf 模式：Infof/Debugf/...（msg 含 %v/%s/%d 等格式動詞）
//
// 後端一律為 log/slog：文字模式走本套件的 textHandler（人類可讀、可選彩色），
// json 模式走 slog.JSONHandler，可直接餵 Loki / Datadog / CloudWatch。
// 層級以 slog.LevelVar 保存，SetLevel 與並行寫入不會互相競爭。
type Logger struct {
	lvl  slog.LevelVar
	json atomic.Bool
	out  *sink
	root *switchHandler
	sl   *slog.Logger
}

// newLogger 建立未設定輸出的 Logger（輸出由呼叫端以 out.set 指定）
func newLogger(level Level, colorEnabled bool) *Logger {
	l := &Logger{out: &sink{colorOn: colorEnabled}}
	l.lvl.Set(level.toSlog())
	l.root = &switchHandler{
		l:    l,
		text: &textHandler{s: l.out},
		json: slog.NewJSONHandler(l.out, &slog.HandlerOptions{Level: &l.lvl}),
		// ReplaceAttr 會讓 slog 對每個屬性多走一次替換，只給自訂層級用
		jsonCustom: slog.NewJSONHandler(l.out, &slog.HandlerOptions{Level: &l.lvl, ReplaceAttr: jsonLevelName}),
	}
	l.sl = slog.New(l.root)
	return l
}

// New 建立 Logger（向後相容的建構子）。
//
// 輸出目的地依序為：writer（非 nil 時）、output 為 "stdout" / "stderr" / ""（stderr），
// 其餘 output 視為檔案路徑，以預設輪轉設定（100MB、保留 10 份、7 天）寫入。
// 先前檔案路徑會被靜默改寫到 stderr。開檔失敗時回傳錯誤，但仍回傳寫到 stderr 的可用實例（不會回傳 nil）。
func New(level string, output string, writer io.Writer, colorEnabled bool) (*Logger, error) {
	l := newLogger(parseLevel(level), colorEnabled)
	switch {
	case writer != nil:
		l.out.set(writer, nil)
	case strings.EqualFold(output, "stdout"):
		l.out.set(os.Stdout, nil)
	case output == "" || strings.EqualFold(output, "stderr"):
		l.out.set(os.Stderr, nil)
	default:
		rot, err := NewRotation(output, defaultRotationConfig())
		if err != nil {
			l.out.set(os.Stderr, nil)
			return l, fmt.Errorf("logger: open %s: %w", output, err)
		}
		l.out.set(rot, rot)
	}
	return l, nil
}

// NewWithRotation 建立寫入 filename 且自動輪轉的 Logger。
// 輪轉依 config（max_size / max_age / max_backups / compress）在寫入路徑內自動觸發；
// Logger.Close 會一併關閉輪轉檔案。
func NewWithRotation(level, filename string, rc *RotationConfig, colorEnabled bool) (*Logger, error) {
	rot, err := NewRotation(filename, rc)
	if err != nil {
		return nil, err
	}
	l := newLogger(parseLevel(level), colorEnabled)
	l.out.set(rot, rot)
	return l, nil
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

// NewLogger 建立新的Logger實例（INFO、stdout、終端機上彩色）
func NewLogger() *Logger {
	l := newLogger(INFO, true)
	l.out.set(os.Stdout, nil)
	return l
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

// Slog 回傳共用同一輸出、層級與格式的 *slog.Logger（之後的 SetLevel / SetFormat /
// SetOutput 也會生效），供需要標準 slog 介面的套件使用。nil Logger 回傳丟棄一切的 logger。
func (l *Logger) Slog() *slog.Logger {
	if l == nil || l.sl == nil {
		return slog.New(slog.DiscardHandler)
	}
	return l.sl
}

// SetLevel 設定日誌級別
func (l *Logger) SetLevel(level Level) {
	if l == nil {
		return
	}
	l.lvl.Set(level.toSlog())
}

// SetOutput 設定輸出。先前由 Logger 自行開啟的檔案（SetFile / 輪轉）會一併關閉；
// Close 之後呼叫會重新啟用輸出。
func (l *Logger) SetOutput(w io.Writer) {
	if l == nil || l.out == nil {
		return
	}
	l.out.set(w, nil)
}

// SetFormat 設定輸出格式："json" 啟用 slog 的 JSON 結構化輸出，其餘維持文字模式。
// json 模式適合生產環境接 Loki / Datadog 等日誌聚合器。
func (l *Logger) SetFormat(format string) {
	if l == nil {
		return
	}
	l.json.Store(strings.EqualFold(format, "json"))
}

// errNilLogger SetFile 等需要實例的操作在 nil / 零值 Logger 上回傳此錯誤
var errNilLogger = errors.New("logger: nil Logger")

// SetFile 設定日誌文件（附加寫入、不輪轉）。
// 開檔失敗時回傳錯誤並保留原輸出；先前會先關掉舊檔，失敗後輸出停在已關閉的檔案上。
func (l *Logger) SetFile(filename string) error {
	if l == nil || l.out == nil {
		return errNilLogger
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	l.out.set(file, file)
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
		return slogNotice
	case WARNING:
		return slog.LevelWarn
	case ERROR:
		return slog.LevelError
	case EMERGENCY:
		return slogEmergency
	default:
		return slog.LevelInfo
	}
}

// fromSlog 將任意 slog.Level 歸入最接近（不高於）的框架 Level
func fromSlog(lv slog.Level) Level {
	switch {
	case lv < slog.LevelInfo:
		return DEBUG
	case lv < slogNotice:
		return INFO
	case lv < slog.LevelWarn:
		return NOTICE
	case lv < slog.LevelError:
		return WARNING
	case lv < slogEmergency:
		return ERROR
	default:
		return EMERGENCY
	}
}

// jsonLevelName 讓 JSON 輸出的自訂層級顯示 "NOTICE" / "EMERGENCY"；
// 先前顯示 slog 預設的 "INFO+2" / "ERROR+4"。標準層級維持 slog 名稱（DEBUG/INFO/WARN/ERROR）。
func jsonLevelName(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if lv, ok := a.Value.Any().(slog.Level); ok {
			switch lv {
			case slogNotice:
				a.Value = slog.StringValue("NOTICE")
			case slogEmergency:
				a.Value = slog.StringValue("EMERGENCY")
			}
		}
	}
	return a
}

// getLevelString 獲取級別字串
func (l *Logger) getLevelString(level Level) string {
	return levelString(level)
}

func levelString(level Level) string {
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

// levelColor 獲取級別顏色
func levelColor(level Level) string {
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

// log 結構化 KV 日誌的共用實作（奇數個參數時最後一個 key 的值為 "(MISSING)"）
func (l *Logger) log(level Level, msg string, keysAndValues ...interface{}) {
	if l == nil || l.root == nil {
		return
	}
	lv := level.toSlog()
	if lv < l.lvl.Level() {
		return
	}
	r := slog.NewRecord(time.Now(), lv, msg, 0)
	for i := 0; i < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			key = fmt.Sprint(keysAndValues[i])
		}
		if i+1 < len(keysAndValues) {
			r.AddAttrs(slog.Any(key, keysAndValues[i+1]))
		} else {
			r.AddAttrs(slog.String(key, "(MISSING)"))
		}
	}
	_ = l.root.Handle(stdcontext.Background(), r)
}

// logf printf-style 日誌的共用實作（msg 含格式動詞，args 為對應參數）
func (l *Logger) logf(level Level, format string, args ...interface{}) {
	if l == nil || l.root == nil {
		return
	}
	lv := level.toSlog()
	if lv < l.lvl.Level() {
		return
	}
	r := slog.NewRecord(time.Now(), lv, fmt.Sprintf(format, args...), 0)
	_ = l.root.Handle(stdcontext.Background(), r)
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

// osExit 供測試替換
var osExit = os.Exit

// Fatal 輸出致命錯誤、關閉日誌檔後退出（KV 模式）。
// 先前直接 os.Exit，輪轉檔案未關閉。
func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.log(EMERGENCY, msg, keysAndValues...)
	l.Close()
	osExit(1)
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

// Fatalf 以 printf 格式輸出致命錯誤、關閉日誌檔後退出
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logf(EMERGENCY, format, args...)
	l.Close()
	osExit(1)
}

// Close 關閉 Logger 自行開啟的檔案（SetFile / 輪轉）；之後的日誌一律丟棄，不會重新開檔。
// 重複呼叫安全。先前 Close 後再寫入，輪轉 writer 會把檔案重新打開。
func (l *Logger) Close() {
	if l == nil || l.out == nil {
		return
	}
	l.out.close()
}

// ===== 輸出目的地 =====

// sink 是所有 handler 共用的輸出目的地：序列化寫入、Close 後丟棄、
// 換輸出時關閉先前由 Logger 自行開啟的檔案，並決定是否輸出色碼。
type sink struct {
	mu      sync.Mutex
	w       io.Writer
	closer  io.Closer // Logger 自行開啟的檔案或輪轉 writer
	closed  bool
	colorOn bool        // 使用者設定（colors: true）
	color   atomic.Bool // colorOn 且目的地為終端機
}

func (s *sink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.w == nil {
		return len(p), nil
	}
	return s.w.Write(p)
}

// set 切換輸出；closer 為 Logger 擁有、需在換掉或 Close 時關閉的資源
func (s *sink) set(w io.Writer, closer io.Closer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closer != nil && s.closer != closer {
		s.closer.Close()
	}
	s.w, s.closer, s.closed = w, closer, false
	// 先前只要 colors 為 true 就輸出色碼，寫進檔案、Docker 日誌與管線
	s.color.Store(s.colorOn && isTerminal(w))
}

func (s *sink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.closer != nil {
		s.closer.Close()
		s.closer = nil
	}
}

// isTerminal 判斷 w 是否為終端機（字元裝置）；供測試替換
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// switchHandler 依 SetFormat 在文字 / JSON handler 間切換，層級由 Logger 的 LevelVar 決定
type switchHandler struct {
	l          *Logger
	text       slog.Handler
	json       slog.Handler // 標準層級
	jsonCustom slog.Handler // NOTICE / EMERGENCY（改寫層級名稱）
}

func (h *switchHandler) Enabled(_ stdcontext.Context, lv slog.Level) bool {
	return lv >= h.l.lvl.Level()
}

func (h *switchHandler) Handle(ctx stdcontext.Context, r slog.Record) error {
	if h.l.json.Load() {
		if r.Level == slogNotice || r.Level == slogEmergency {
			return h.jsonCustom.Handle(ctx, r)
		}
		return h.json.Handle(ctx, r)
	}
	return h.text.Handle(ctx, r)
}

func (h *switchHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &switchHandler{l: h.l, text: h.text.WithAttrs(attrs), json: h.json.WithAttrs(attrs), jsonCustom: h.jsonCustom.WithAttrs(attrs)}
}

func (h *switchHandler) WithGroup(name string) slog.Handler {
	return &switchHandler{l: h.l, text: h.text.WithGroup(name), json: h.json.WithGroup(name), jsonCustom: h.jsonCustom.WithGroup(name)}
}
