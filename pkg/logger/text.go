// @chris
package logger

import (
	stdcontext "context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// textTimeFormat 文字模式時間戳記：RFC3339 + 毫秒 + 時區（先前為 "2006/01/02 15:04:05"）
const textTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// textHandler 人類可讀的 slog handler，輸出格式：
//
//	2026-10-26T14:03:05.123+08:00 [INFO] request done | status=200 path="/a b"
//
// 值含空白、=、引號、控制字元或為空字串時以 Go 字串語法加引號跳脫；
// 訊息含控制字元或 " | " 分隔符時整段加引號。先前原樣輸出，值裡的 \n 可偽造整行日誌。
type textHandler struct {
	s      *sink
	pre    []byte // WithAttrs 預先格式化的 " key=value"
	prefix string // WithGroup 的群組前綴（"g1.g2."）
}

var textBufPool = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

func (h *textHandler) Enabled(stdcontext.Context, slog.Level) bool { return true }

func (h *textHandler) Handle(_ stdcontext.Context, r slog.Record) error {
	bp := textBufPool.Get().(*[]byte)
	buf := (*bp)[:0]

	if !r.Time.IsZero() {
		buf = r.Time.AppendFormat(buf, textTimeFormat)
		buf = append(buf, ' ')
	}
	level := fromSlog(r.Level)
	if h.s.color.Load() {
		buf = append(buf, levelColor(level)...)
		buf = append(buf, '[')
		buf = append(buf, levelString(level)...)
		buf = append(buf, ']')
		buf = append(buf, ColorReset...)
	} else {
		buf = append(buf, '[')
		buf = append(buf, levelString(level)...)
		buf = append(buf, ']')
	}
	buf = append(buf, ' ')
	if msgNeedsQuote(r.Message) {
		buf = strconv.AppendQuote(buf, r.Message)
	} else {
		buf = append(buf, r.Message...)
	}

	if len(h.pre) > 0 || r.NumAttrs() > 0 {
		buf = append(buf, " |"...)
		buf = append(buf, h.pre...)
		r.Attrs(func(a slog.Attr) bool {
			buf = appendAttr(buf, h.prefix, a)
			return true
		})
	}
	buf = append(buf, '\n')

	_, err := h.s.Write(buf)
	if cap(buf) <= 64<<10 { // 過大的緩衝不放回池中
		*bp = buf
		textBufPool.Put(bp)
	}
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	pre := append([]byte(nil), h.pre...)
	for _, a := range attrs {
		pre = appendAttr(pre, h.prefix, a)
	}
	return &textHandler{s: h.s, pre: pre, prefix: h.prefix}
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &textHandler{s: h.s, pre: h.pre, prefix: h.prefix + name + "."}
}

// appendAttr 以 " key=value" 附加一個屬性；群組攤平為 "group.key=value"
func appendAttr(buf []byte, prefix string, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return buf
	}
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range attrs {
			buf = appendAttr(buf, prefix, ga)
		}
		return buf
	}

	buf = append(buf, ' ')
	if prefix == "" {
		buf = appendMaybeQuoted(buf, a.Key)
	} else {
		buf = appendMaybeQuoted(buf, prefix+a.Key)
	}
	buf = append(buf, '=')
	return appendValue(buf, a.Value)
}

// appendValue 依值的種類輸出；一般型別沿用 %v 的呈現方式
func appendValue(buf []byte, v slog.Value) []byte {
	switch v.Kind() {
	case slog.KindString:
		return appendMaybeQuoted(buf, v.String())
	case slog.KindInt64:
		return strconv.AppendInt(buf, v.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(buf, v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.AppendFloat(buf, v.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.AppendBool(buf, v.Bool())
	case slog.KindDuration:
		return append(buf, v.Duration().String()...)
	case slog.KindTime:
		return v.Time().AppendFormat(buf, time.RFC3339Nano)
	default:
		start := len(buf)
		buf = fmt.Appendf(buf, "%v", v.Any())
		if needsQuote(buf[start:]) {
			s := string(buf[start:])
			buf = strconv.AppendQuote(buf[:start], s)
		}
		return buf
	}
}

func appendMaybeQuoted(buf []byte, s string) []byte {
	if needsQuote(s) {
		return strconv.AppendQuote(buf, s)
	}
	return append(buf, s...)
}

// needsQuote 空字串，或含空白、=、引號、控制字元、無效 UTF-8 / 不可列印字元時需加引號
func needsQuote[T string | []byte](s T) bool {
	if len(s) == 0 {
		return true
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c <= ' ' || c == '=' || c == '"' || c == 0x7f {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(string(s[i:min(i+utf8.UTFMax, len(s))]))
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return true
		}
		i += size
	}
	return false
}

// msgNeedsQuote 訊息含控制字元（可偽造新行）或屬性分隔符 " | " 時需加引號；一般空白不需要
func msgNeedsQuote(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < ' ' || c == 0x7f {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError || (!unicode.IsPrint(r) && r != '　') {
			return true
		}
		i += size
	}
	return strings.Contains(s, " | ")
}
