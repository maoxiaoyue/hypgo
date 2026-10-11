// @chris
package context

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"unicode/utf16"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ===== Render 介面 =====

// Render 渲染介面
type Render interface {
	Render(http.ResponseWriter) error
	WriteContentType(w http.ResponseWriter)
}

// ===== JSON 渲染器 =====

type jsonRender struct{ Data interface{} }

func (r jsonRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	buf, err := encodeJSON(r.Data, true)
	if err != nil {
		return err
	}
	defer ReleaseBuffer(buf)
	_, err = w.Write(buf.Bytes())
	return err
}

// encodeJSON 把 v 編進池化緩衝（呼叫端負責 ReleaseBuffer）。
// 池化緩衝 + Encoder：json.Marshal 每次回應都整包新配置 body，
// 是典型小 JSON 回應在 H1-H3 修復後的最大單筆每請求分配。
// 先編完再寫 w：失敗時尚未送出任何位元組，Render 的 panic 才能由 Recovery 轉成乾淨的 500
func encodeJSON(v interface{}, escapeHTML bool) (*bytes.Buffer, error) {
	buf := AcquireBuffer()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(v); err != nil {
		ReleaseBuffer(buf)
		return nil, err
	}
	// Encoder 尾端固定補 '\n'，去除以維持與 json.Marshal 相同輸出
	if b := buf.Bytes(); len(b) > 0 && b[len(b)-1] == '\n' {
		buf.Truncate(len(b) - 1)
	}
	return buf, nil
}

func (r jsonRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// ===== Indented JSON 渲染器 =====

type indentedJSONRender struct{ Data interface{} }

func (r indentedJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	jsonBytes, err := json.MarshalIndent(r.Data, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(jsonBytes)
	return err
}

func (r indentedJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// ===== Secure JSON 渲染器 =====

type secureJSONRender struct {
	Prefix string
	Data   interface{}
}

func (r secureJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	jsonBytes, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	if r.Prefix != "" {
		_, err = w.Write([]byte(r.Prefix))
		if err != nil {
			return err
		}
	}
	_, err = w.Write(jsonBytes)
	return err
}

func (r secureJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// ===== JSONP 渲染器 =====

type jsonpJSONRender struct {
	Callback string
	Data     interface{}
}

func (r jsonpJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	jsonBytes, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	callback := template.JSEscapeString(r.Callback)
	_, err = fmt.Fprintf(w, "%s(%s);", callback, jsonBytes)
	return err
}

func (r jsonpJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/javascript; charset=utf-8"})
}

// ===== ASCII JSON 渲染器 =====

type asciiJSONRender struct{ Data interface{} }

func (r asciiJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	buf, err := encodeJSON(r.Data, true)
	if err != nil {
		return err
	}
	defer ReleaseBuffer(buf)
	_, err = w.Write(escapeNonASCII(buf.Bytes()))
	return err
}

// escapeNonASCII 把非 ASCII 字元轉成 JSON 的 \uXXXX（BMP 以外用 UTF-16 代理對），輸出保證純 ASCII。
// 先前 AsciiJSON 只是 SetEscapeHTML(false)，與 PureJSON 完全相同、中文原樣輸出
func escapeNonASCII(b []byte) []byte {
	i := 0
	for i < len(b) && b[i] < utf8.RuneSelf {
		i++
	}
	if i == len(b) {
		return b
	}
	out := make([]byte, 0, 2*len(b))
	out = append(out, b[:i]...)
	for _, r := range string(b[i:]) {
		switch {
		case r < utf8.RuneSelf:
			out = append(out, byte(r))
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			out = appendUnicodeEscape(appendUnicodeEscape(out, hi), lo)
		default:
			out = appendUnicodeEscape(out, r)
		}
	}
	return out
}

func appendUnicodeEscape(out []byte, r rune) []byte {
	const hex = "0123456789abcdef"
	return append(out, '\\', 'u', hex[r>>12&0xf], hex[r>>8&0xf], hex[r>>4&0xf], hex[r&0xf])
}

func (r asciiJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// ===== Pure JSON 渲染器 =====

type pureJSONRender struct{ Data interface{} }

func (r pureJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	buf, err := encodeJSON(r.Data, false)
	if err != nil {
		return err
	}
	defer ReleaseBuffer(buf)
	_, err = w.Write(buf.Bytes())
	return err
}

func (r pureJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// ===== XML 渲染器 =====

type xmlRender struct{ Data interface{} }

// Render 先 Marshal 再寫：串流 Encoder 遇到不可序列化的欄位時，前段內容已隨 bufio 分塊送出、
// header 已是 200，Recovery 的 500 再也送不出去
func (r xmlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	b, err := xml.Marshal(r.Data)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (r xmlRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/xml; charset=utf-8"})
}

// ===== YAML 渲染器 =====

type yamlRender struct{ Data interface{} }

func (r yamlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	b, err := yaml.Marshal(r.Data) // 同 xmlRender：先編完再寫
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (r yamlRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/x-yaml; charset=utf-8"})
}

// ===== String 渲染器 =====
type stringRender struct {
	Format string
	Data   []interface{}
}

func (r stringRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	output := r.Format
	if len(r.Data) > 0 {
		output = fmt.Sprintf(r.Format, r.Data...)
	}
	_, err := w.Write([]byte(output))
	return err
}

func (r stringRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"text/plain; charset=utf-8"})
}

// ===== Redirect 渲染器 =====

type redirectRender struct {
	Code     int
	Location string
	Request  *http.Request
}

func (r redirectRender) Render(w http.ResponseWriter) error {
	if (r.Code < http.StatusMultipleChoices || r.Code > http.StatusPermanentRedirect) && r.Code != http.StatusCreated {
		panic(fmt.Sprintf("Cannot redirect with status code %d", r.Code))
	}
	http.Redirect(w, r.Request, r.Location, r.Code)
	return nil
}

func (r redirectRender) WriteContentType(http.ResponseWriter) {}

// ===== Data 渲染器 =====

type dataRender struct {
	ContentType string
	Data        []byte
}

func (r dataRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	_, err := w.Write(r.Data)
	return err
}

func (r dataRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{r.ContentType})
}

// ===== HTML 渲染器 =====

// htmlTemplates 全域 HTML 樣板集（由 router.LoadHTMLGlob / SetHTMLTemplate 設定）
var htmlTemplates atomic.Pointer[template.Template]

// SetHTMLTemplate 設定 c.HTML 使用的全域樣板集（啟動時呼叫一次；線程安全）
func SetHTMLTemplate(t *template.Template) {
	htmlTemplates.Store(t)
}

type htmlRender struct {
	Template *template.Template
	Name     string
	Data     interface{}
}

func (r htmlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	if r.Name == "" {
		return r.Template.Execute(w, r.Data)
	}
	return r.Template.ExecuteTemplate(w, r.Name, r.Data)
}

func (r htmlRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"text/html; charset=utf-8"})
}

// ===== Reader 渲染器 =====

type readerRender struct {
	Headers       map[string]string
	ContentType   string
	ContentLength int64
	Reader        io.Reader
}

func (r readerRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	if r.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	}
	for k, v := range r.Headers {
		w.Header().Set(k, v)
	}
	_, err := io.Copy(w, r.Reader)
	return err
}

func (r readerRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{r.ContentType})
}

// ===== Server-Sent Event 渲染器 =====
type sseventRender struct {
	Event string
	Data  interface{}
}

func (r sseventRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	if r.Event != "" {
		fmt.Fprintf(w, "event: %s\n", r.Event)
	}
	jsonBytes, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonBytes)

	// 立即刷新
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (r sseventRender) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

// ===== 輔助函數 =====

// writeContentType 寫入 Content-Type
func writeContentType(w http.ResponseWriter, value []string) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = value
	}
}

// 註：ProtoBuf / TOML renderer 與其對應的 c.ProtoBuf()/c.TOML() 方法
// 已於 v0.8.11 移除——兩者的 Render 都是無條件回傳 "not implemented"，
// 而 Context.Render 對非 nil error 一律 panic，等於「公開 API 呼叫即崩潰」。
// 需要這兩種格式時請自行 c.Data(code, contentType, bytes)。
