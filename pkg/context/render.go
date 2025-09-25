package context

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ===== Render 介面 =====

// Render 渲染介面
type Render interface {
	Render(http.ResponseWriter) error
	WriteContentType(w http.ResponseWriter)
}

// 預定義的渲染器實例
var render = defaultRender{}

type defaultRender struct {
	JSON         jsonRender
	XML          xmlRender
	YAML         yamlRender
	ProtoBuf     protoBufRender
	IndentedJSON indentedJSONRender
	SecureJSON   secureJSONRender
	JsonpJSON    jsonpJSONRender
	AsciiJSON    asciiJSONRender
}

// ===== JSON 渲染器 =====

type jsonRender struct{ Data interface{} }

func (r jsonRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	jsonBytes, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	_, err = w.Write(jsonBytes)
	return err
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

// IndentedJSON 創建格式化 JSON 渲染器
var IndentedJSON = indentedJSONRender{}

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

// SecureJSON 創建安全 JSON 渲染器
var SecureJSON = secureJSONRender{Prefix: "while(1);"}

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

// JsonpJSON 創建 JSONP 渲染器
var JsonpJSON = jsonpJSONRender{}

// ===== ASCII JSON 渲染器 =====

type asciiJSONRender struct{ Data interface{} }

func (r asciiJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(r.Data)
}

func (r asciiJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// AsciiJSON 創建 ASCII JSON 渲染器
var AsciiJSON = asciiJSONRender{}

// ===== Pure JSON 渲染器 =====

type pureJSONRender struct{ Data interface{} }

func (r pureJSONRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(r.Data)
}

func (r pureJSONRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/json; charset=utf-8"})
}

// PureJSON 創建純 JSON 渲染器
var PureJSON = pureJSONRender{}

// ===== XML 渲染器 =====

type xmlRender struct{ Data interface{} }

func (r xmlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	return xml.NewEncoder(w).Encode(r.Data)
}

func (r xmlRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/xml; charset=utf-8"})
}

// ===== YAML 渲染器 =====

type yamlRender struct{ Data interface{} }

func (r yamlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	return yaml.NewEncoder(w).Encode(r.Data)
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

// String 創建字符串渲染器
var String = stringRender{}

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

// Redirect 創建重定向渲染器
var Redirect = redirectRender{}

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

// Data 創建數據渲染器
var Data = dataRender{}

// ===== HTML 渲染器 =====

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

// HTML 創建 HTML 渲染器
var HTML = htmlRender{}

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

// Reader 創建 Reader 渲染器
var Reader = readerRender{}

// ===== ProtoBuf 渲染器 =====

type protoBufRender struct{ Data interface{} }

func (r protoBufRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	// 需要 protobuf 庫支援
	return fmt.Errorf("protobuf render not implemented")
}

func (r protoBufRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/x-protobuf"})
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

// SSEvent 創建 SSE 渲染器
var SSEvent = sseventRender{}

// ===== 輔助函數 =====

// writeContentType 寫入 Content-Type
func writeContentType(w http.ResponseWriter, value []string) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = value
	}
}

// ===== TOML 渲染器 =====

type tomlRender struct{ Data interface{} }

func (r tomlRender) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	// 需要 toml 庫支援
	return fmt.Errorf("toml render not implemented")
}

func (r tomlRender) WriteContentType(w http.ResponseWriter) {
	writeContentType(w, []string{"application/toml; charset=utf-8"})
}

// TOML 創建 TOML 渲染器
var TOML = tomlRender{}
