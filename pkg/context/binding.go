// @chris
package context

import (
	"bytes"
	"encoding"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

// ===== 綁定方法 =====

// Bind 綁定請求資料到結構體（會自動選擇綁定器）
func (c *Context) Bind(obj interface{}) error {
	b, err := binding.For(c.Request)
	if err != nil {
		c.AbortWithError(http.StatusUnsupportedMediaType, err).SetType(ErrorTypeBind)
		return err
	}
	return c.MustBindWith(obj, b)
}

// BindJSON 綁定 JSON 資料到結構體
func (c *Context) BindJSON(obj interface{}) error {
	return c.MustBindWith(obj, bindingJSON{})
}

// BindXML 綁定 XML 資料到結構體
func (c *Context) BindXML(obj interface{}) error {
	return c.MustBindWith(obj, bindingXML{})
}

// BindQuery 綁定查詢參數到結構體
func (c *Context) BindQuery(obj interface{}) error {
	return c.MustBindWith(obj, bindingQuery{})
}

// BindYAML 綁定 YAML 資料到結構體
func (c *Context) BindYAML(obj interface{}) error {
	return c.MustBindWith(obj, bindingYAML{})
}

// BindHeader 綁定 Header 到結構體
func (c *Context) BindHeader(obj interface{}) error {
	return c.MustBindWith(obj, bindingHeader{})
}

// BindUri 綁定 URI 參數到結構體
func (c *Context) BindUri(obj interface{}) error {
	if c.Params != nil {
		m := make(map[string][]string)
		for _, p := range c.Params {
			m[p.Key] = []string{p.Value}
		}
		return bindingUri{}.BindUri(m, obj)
	}
	return nil
}

// BindWith 綁定請求數據（舊版兼容）
func (c *Context) BindWith(obj interface{}, b Binding) error {
	return c.MustBindWith(obj, b)
}

// MustBindWith 強制綁定（失敗會 abort）
func (c *Context) MustBindWith(obj interface{}, b Binding) error {
	if err := c.ShouldBindWith(obj, b); err != nil {
		status := http.StatusBadRequest
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			status = http.StatusRequestEntityTooLarge
		}
		c.AbortWithError(status, err).SetType(ErrorTypeBind)
		return err
	}
	return nil
}

// ===== Should 系列（不會 abort）=====

// ShouldBind 嘗試綁定請求資料（不會 abort）
func (c *Context) ShouldBind(obj interface{}) error {
	b, err := binding.For(c.Request)
	if err != nil {
		return err
	}
	return c.ShouldBindWith(obj, b)
}

// ShouldBindJSON 嘗試綁定 JSON（不會 abort）
func (c *Context) ShouldBindJSON(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingJSON{})
}

// ShouldBindXML 嘗試綁定 XML（不會 abort）
func (c *Context) ShouldBindXML(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingXML{})
}

// ShouldBindQuery 嘗試綁定查詢參數（不會 abort）
func (c *Context) ShouldBindQuery(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingQuery{})
}

// ShouldBindYAML 嘗試綁定 YAML（不會 abort）
func (c *Context) ShouldBindYAML(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingYAML{})
}

// ShouldBindHeader 嘗試綁定 Header（不會 abort）
func (c *Context) ShouldBindHeader(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingHeader{})
}

// ShouldBindUri 嘗試綁定 URI 參數（不會 abort）
func (c *Context) ShouldBindUri(obj interface{}) error {
	if c.Params != nil {
		m := make(map[string][]string)
		for _, p := range c.Params {
			m[p.Key] = []string{p.Value}
		}
		return bindingUri{}.BindUri(m, obj)
	}
	return nil
}

// ShouldBindWith 使用指定的綁定器綁定
func (c *Context) ShouldBindWith(obj interface{}, b Binding) error {
	return b.Bind(c.Request, obj)
}

// ShouldBindWithQuery 使用查詢綁定器
func (c *Context) ShouldBindWithQuery(obj interface{}) error {
	return c.ShouldBindWith(obj, bindingQuery{})
}

// ShouldBindBodyWith 綁定請求體並緩存（可多次綁定）
func (c *Context) ShouldBindBodyWith(obj interface{}, bb BindingBody) error {
	var body []byte
	var err error

	if c.rawData != nil {
		body = c.rawData
	} else {
		body, err = readBodyWithHint(c.Request.Body, c.Request.ContentLength)
		if err != nil {
			return err
		}
		c.rawData = body
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	return bb.BindBody(body, obj)
}

// ===== Map 綁定 =====

// ShouldBindQueryMap 綁定查詢參數到 map
func (c *Context) ShouldBindQueryMap(m *map[string]string) error {
	*m = make(map[string]string)
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			(*m)[k] = v[0]
		}
	}
	return nil
}

// ShouldBindFormMap 綁定表單到 map
func (c *Context) ShouldBindFormMap(m *map[string]string) error {
	*m = make(map[string]string)
	if err := c.Request.ParseForm(); err != nil {
		return err
	}
	for k, v := range c.Request.PostForm {
		if len(v) > 0 {
			(*m)[k] = v[0]
		}
	}
	return nil
}

// ===== Binding 介面 =====

// Binding 描述能夠綁定請求數據的介面
type Binding interface {
	Name() string
	Bind(*http.Request, interface{}) error
}

// BindingBody 添加 BindBody 方法
type BindingBody interface {
	Binding
	BindBody([]byte, interface{}) error
}

// BindingUri 添加 BindUri 方法
type BindingUri interface {
	Name() string
	BindUri(map[string][]string, interface{}) error
}

// ===== 綁定器實例 =====

// 預定義的綁定器實例
var binding = defaultBinding{}

type defaultBinding struct{}

// ErrUnsupportedMediaType 請求帶了 body，但 Content-Type 不是任何綁定器認得的型別
var ErrUnsupportedMediaType = errors.New("unsupported Content-Type")

// Default 依方法與 Content-Type 選綁定器；認不得的型別回 Form（相容舊行為）。
// ShouldBind / Bind / BindInput 改用 For，它對認不得的型別回錯
func (defaultBinding) Default(method, contentType string) Binding {
	if method == http.MethodGet || method == http.MethodHead {
		return Query
	}
	if b := bindingForMedia(contentType); b != nil {
		return b
	}
	return Form
}

// For 依請求選綁定器。
//
//   - GET / HEAD 沒有 body：綁 query string（Form 綁定器只讀 PostForm，對 GET 永遠是空的）
//   - 依 Content-Type：不分大小寫、忽略參數，接受 +json / +xml 結尾（application/merge-patch+json）
//   - 沒有 body：DELETE 綁 query string（先前 DELETE /items?force=true 綁出全零值），其餘走 Form
//   - 有 body 而型別認不得（含沒帶 Content-Type）：回 ErrUnsupportedMediaType
//
// 先前認不得的型別一律當成表單：Application/JSON、application/vnd.api+json、text/plain
// 送來的 JSON 被當表單解析、什麼都沒綁到，handler 拿著全零值的 struct 回 200
func (defaultBinding) For(req *http.Request) (Binding, error) {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return Query, nil
	}
	contentType := req.Header.Get("Content-Type")
	if b := bindingForMedia(contentType); b != nil {
		return b, nil
	}
	if !hasBody(req) {
		if req.Method == http.MethodDelete {
			return Query, nil
		}
		return Form, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnsupportedMediaType, contentType)
}

// hasBody 回報請求有沒有 body。長度未知（ContentLength -1）時讀一個位元組確認：
// HTTP/3 沒帶 Content-Length 的請求（quic-go 一律掛 stream body）與 HTTP/1.1 的 chunked 空 body
// 都是 -1。先前只看 ContentLength == 0，這兩種沒有 body 的 POST / PUT / DELETE 被回 415
func hasBody(req *http.Request) bool {
	if req.ContentLength == 0 || req.Body == nil || req.Body == http.NoBody {
		return false
	}
	if req.ContentLength > 0 {
		return true
	}
	var first [1]byte
	n, _ := io.ReadFull(req.Body, first[:])
	if n == 0 {
		return false
	}
	// 把讀掉的位元組接回去，之後讀 body 的人看到的內容不變
	req.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(first[:]), req.Body), req.Body}
	return true
}

// bindingForMedia 回傳 Content-Type 對應的綁定器；認不得時回 nil
func bindingForMedia(contentType string) Binding {
	media := strings.ToLower(strings.TrimSpace(filterFlags(strings.TrimSpace(contentType))))
	switch {
	case media == MIMEJSON || strings.HasSuffix(media, "+json"):
		return JSON
	case media == MIMEXML || media == MIMEXML2 || strings.HasSuffix(media, "+xml"):
		return XML
	case media == MIMEPROTOBUF || media == "application/protobuf":
		return ProtoBuf
	case media == MIMEMSGPACK || media == MIMEMSGPACK2:
		return MsgPack
	case media == MIMEYAML || media == MIMEYAML2 || media == "text/yaml":
		return YAML
	case media == MIMEMultipartPOSTForm:
		return FormMultipart
	case media == MIMEPOSTForm:
		return Form
	}
	return nil
}

// 各種綁定器常量
var (
	JSON          = bindingJSON{}
	XML           = bindingXML{}
	Form          = bindingForm{}
	Query         = bindingQuery{}
	FormPost      = bindingFormPost{}
	FormMultipart = bindingFormMultipart{}
	ProtoBuf      = bindingProtoBuf{}
	MsgPack       = bindingMsgPack{}
	YAML          = bindingYAML{}
	Uri           = bindingUri{}
	Header        = bindingHeader{}
)

// MIME 類型常量

// ===== JSON 綁定器 =====

type bindingJSON struct{}

func (bindingJSON) Name() string { return "json" }

func (bindingJSON) Bind(req *http.Request, obj interface{}) error {
	if req == nil || req.Body == nil {
		return fmt.Errorf("invalid request")
	}
	return decodeJSON(req.Body, obj)
}

func (bindingJSON) BindBody(body []byte, obj interface{}) error {
	return decodeJSON(bytes.NewReader(body), obj)
}

// ===== XML 綁定器 =====

type bindingXML struct{}

func (bindingXML) Name() string { return "xml" }

func (bindingXML) Bind(req *http.Request, obj interface{}) error {
	if req.Body == nil {
		return fmt.Errorf("invalid request")
	}
	return xml.NewDecoder(req.Body).Decode(obj)
}

func (bindingXML) BindBody(body []byte, obj interface{}) error {
	return xml.Unmarshal(body, obj)
}

// ===== Form 綁定器 =====

type bindingForm struct{}

func (bindingForm) Name() string { return "form" }

func (bindingForm) Bind(req *http.Request, obj interface{}) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	// 用 PostForm（純 body）而非 Form（body ∪ query）：
	// Form 會讓 query string 也能寫進 struct，形成大量賦值漏洞
	// （POST /api/users?is_admin=true 可注入 body 沒有的欄位）。
	// 需要 query 時請改用 BindQuery。
	return mapFormToStruct(req.PostForm, obj)
}

// ===== Query 綁定器 =====

type bindingQuery struct{}

func (bindingQuery) Name() string { return "query" }

func (bindingQuery) Bind(req *http.Request, obj interface{}) error {
	values := req.URL.Query()
	return mapFormToStruct(values, obj)
}

// ===== FormPost 綁定器 =====

type bindingFormPost struct{}

func (bindingFormPost) Name() string { return "form-urlencoded" }

func (bindingFormPost) Bind(req *http.Request, obj interface{}) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	return mapFormToStruct(req.PostForm, obj)
}

// ===== FormMultipart 綁定器 =====

type bindingFormMultipart struct{}

func (bindingFormMultipart) Name() string { return "multipart/form-data" }

func (bindingFormMultipart) Bind(req *http.Request, obj interface{}) error {
	if err := req.ParseMultipartForm(defaultMemory); err != nil {
		return err
	}
	return mapFormToStruct(req.PostForm, obj)
}

// ===== ProtoBuf 綁定器 =====

type bindingProtoBuf struct{}

func (bindingProtoBuf) Name() string { return "protobuf" }

// Bind 把 application/x-protobuf 的 body 解進 obj（必須是 proto.Message）。
// 先前一律回 "not implemented"
func (b bindingProtoBuf) Bind(req *http.Request, obj interface{}) error {
	body, err := readRequestBody(req)
	if err != nil {
		return err
	}
	return b.BindBody(body, obj)
}

func (bindingProtoBuf) BindBody(body []byte, obj interface{}) error {
	m, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("protobuf binding: %T is not a proto.Message", obj)
	}
	return proto.Unmarshal(body, m)
}

// ===== MsgPack 綁定器 =====

type bindingMsgPack struct{}

func (bindingMsgPack) Name() string { return "msgpack" }

// Bind 把 application/msgpack 的 body 解進 obj；欄位名取 json tag，同一個 struct
// 可以同時收 JSON 與 MessagePack。先前一律回 "not implemented"
func (b bindingMsgPack) Bind(req *http.Request, obj interface{}) error {
	body, err := readRequestBody(req)
	if err != nil {
		return err
	}
	return b.BindBody(body, obj)
}

func (bindingMsgPack) BindBody(body []byte, obj interface{}) error {
	dec := msgpack.NewDecoder(bytes.NewReader(body))
	dec.SetCustomStructTag("json")
	return dec.Decode(obj)
}

// readRequestBody 讀完請求 body（供需要整包資料的綁定器使用）
func readRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, fmt.Errorf("invalid request")
	}
	return readBodyWithHint(req.Body, req.ContentLength)
}

// ===== YAML 綁定器 =====

type bindingYAML struct{}

func (bindingYAML) Name() string { return "yaml" }

func (bindingYAML) Bind(req *http.Request, obj interface{}) error {
	if req.Body == nil {
		return fmt.Errorf("invalid request")
	}
	return yaml.NewDecoder(req.Body).Decode(obj)
}

func (bindingYAML) BindBody(body []byte, obj interface{}) error {
	return yaml.Unmarshal(body, obj)
}

// ===== Uri 綁定器 =====

type bindingUri struct{}

func (bindingUri) Name() string { return "uri" }

func (bindingUri) BindUri(m map[string][]string, obj interface{}) error {
	return mapFormToStruct(url.Values(m), obj)
}

// ===== Header 綁定器 =====

type bindingHeader struct{}

func (bindingHeader) Name() string { return "header" }

func (bindingHeader) Bind(req *http.Request, obj interface{}) error {
	// Go 以 canonical 形式存 header key（X-Api-Key）；tag 寫 X-API-Key 也要對得到
	return mapToStruct(url.Values(req.Header), obj, textproto.CanonicalMIMEHeaderKey)
}

// ===== 輔助函數 =====

// errTrailingJSON JSON 值之後還有內容
var errTrailingJSON = errors.New("unexpected data after the JSON value")

// decodeJSON 解碼 JSON；值之後只能有空白。先前 {"a":1}{"admin":true} 與尾端的雜訊都被接受：
// 前面的代理或 WAF 若取最後一個值，兩邊看到的內容就不一樣
func decodeJSON(r io.Reader, obj interface{}) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(obj); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errTrailingJSON
	}
	return nil
}

// mapFormToStruct 以反射把表單值填入 struct，依欄位型別做字串轉換。
//
// 欄位名優先取 `form` tag，其次 `json` tag，最後用欄位名（沒有 tag 時不分大小寫：
// 先前要完全相同，struct{Page int} 配 ?page=2 得到 0）。
// 舊版用「全部值當字串 → json.Marshal → json.Unmarshal」的中介做法，
// 導致任何非字串欄位必定失敗（?page=2 綁 int 欄位回
// "cannot unmarshal string into Go value of type int"），且完全不認 form tag。
func mapFormToStruct(values url.Values, obj interface{}) error {
	return mapToStruct(values, obj, nil)
}

// mapToStruct 同 mapFormToStruct；keyFn 非 nil 時先轉換欄位鍵名再查表（header 綁定用）
func mapToStruct(values url.Values, obj interface{}, keyFn func(string) string) error {
	rv := reflect.ValueOf(obj)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("binding: target must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("binding: target must point to a struct")
	}
	return mapFormToStructValue(values, rv, keyFn)
}

// formFieldInfo 單一欄位的綁定中繼資料（per-type 快取，見 formFieldsOf）
type formFieldInfo struct {
	index     int
	name      string
	anonymous bool // 匿名嵌入 struct：遞迴攤平
	fold      bool // 沒有 tag：鍵名不分大小寫
}

// formFieldCache reflect.Type → []formFieldInfo
// tag 解析結果對同一型別永不改變，先前每次綁定都重跑
// Tag.Get + strings.Split（每欄位每請求一次 []string 分配）
var formFieldCache sync.Map

// formFieldsOf 取得（或建立並快取）型別的欄位綁定中繼資料
func formFieldsOf(rt reflect.Type) []formFieldInfo {
	if v, ok := formFieldCache.Load(rt); ok {
		return v.([]formFieldInfo)
	}
	fields := make([]formFieldInfo, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // 未匯出欄位
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			fields = append(fields, formFieldInfo{index: i, anonymous: true})
			continue
		}
		name, tagged, skip := formFieldName(f)
		if skip {
			continue
		}
		fields = append(fields, formFieldInfo{index: i, name: name, fold: !tagged})
	}
	formFieldCache.Store(rt, fields)
	return fields
}

func mapFormToStructValue(values url.Values, rv reflect.Value, keyFn func(string) string) error {
	for _, fi := range formFieldsOf(rv.Type()) {
		fv := rv.Field(fi.index)
		if !fv.CanSet() {
			continue
		}

		// 匿名嵌入的 struct：遞迴攤平
		if fi.anonymous {
			if err := mapFormToStructValue(values, fv, keyFn); err != nil {
				return err
			}
			continue
		}

		key := fi.name
		if keyFn != nil {
			key = keyFn(key)
		}
		vs, ok := values[key]
		if !ok && fi.fold {
			for k, v := range values {
				if strings.EqualFold(k, key) {
					vs, ok = v, true
					break
				}
			}
		}
		if !ok || len(vs) == 0 {
			continue // 沒給值就保持零值，不覆寫
		}

		if err := setFormValue(fv, vs); err != nil {
			return fmt.Errorf("binding: field %q: %w", fi.name, err)
		}
	}
	return nil
}

// formFieldName 取欄位對應的表單鍵名：form tag > json tag > 欄位名。
// tag 為 "-" 時跳過該欄位；tagged 回報鍵名是否來自 tag。
func formFieldName(f reflect.StructField) (name string, tagged, skip bool) {
	for _, tag := range []string{"form", "json"} {
		if v := f.Tag.Get(tag); v != "" {
			v = strings.Split(v, ",")[0] // 去掉 ,omitempty 等選項
			if v == "-" {
				return "", true, true
			}
			if v != "" {
				return v, true, false
			}
		}
	}
	return f.Name, false, false
}

// setFormValue 依目標欄位型別轉換字串值
func setFormValue(fv reflect.Value, vs []string) error {
	// slice 欄位：吃全部值（[]string / []int ...）
	if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() != reflect.Uint8 {
		out := reflect.MakeSlice(fv.Type(), len(vs), len(vs))
		for i, s := range vs {
			if err := setScalar(out.Index(i), s); err != nil {
				return err
			}
		}
		fv.Set(out)
		return nil
	}
	return setScalar(fv, vs[0])
}

func setScalar(fv reflect.Value, s string) error {
	// 指標欄位：配置後填入指向的值
	if fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return setScalar(fv.Elem(), s)
	}

	// time.Time 與實作 encoding.TextUnmarshaler 的型別（UUID、netip.Addr、自訂列舉…）。
	// 先前落到最後的 JSON 解析：?since=2026-01-02T03:04:05Z 不是合法 JSON，一律 400
	if fv.CanAddr() {
		switch t := fv.Addr().Interface().(type) {
		case *time.Time:
			if s == "" {
				return nil
			}
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
				if v, err := time.Parse(layout, s); err == nil {
					*t = v
					return nil
				}
			}
			return fmt.Errorf("%q is not a time (RFC 3339 or YYYY-MM-DD)", s)
		case encoding.TextUnmarshaler:
			if s == "" {
				return nil
			}
			return t.UnmarshalText([]byte(s))
		}
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		if s == "" {
			fv.SetBool(false)
			return nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if s == "" {
			fv.SetInt(0)
			return nil
		}
		n, err := strconv.ParseInt(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if s == "" {
			fv.SetUint(0)
			return nil
		}
		n, err := strconv.ParseUint(s, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if s == "" {
			fv.SetFloat(0)
			return nil
		}
		f, err := strconv.ParseFloat(s, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		// 其餘型別（struct、map…）退回 JSON 解析，支援 ?filter={"a":1} 這類用法
		return json.Unmarshal([]byte(s), fv.Addr().Interface())
	}
	return nil
}
