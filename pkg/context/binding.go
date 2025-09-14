package context

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"gopkg.in/yaml.v3"
)

// ===== 綁定方法 =====

// Bind 綁定請求資料到結構體（會自動選擇綁定器）
func (c *Context) Bind(obj interface{}) error {
	b := binding.Default(c.Request.Method, c.ContentType())
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
		c.AbortWithError(http.StatusBadRequest, err).SetType(ErrorTypeBind)
		return err
	}
	return nil
}

// ===== Should 系列（不會 abort）=====

// ShouldBind 嘗試綁定請求資料（不會 abort）
func (c *Context) ShouldBind(obj interface{}) error {
	b := binding.Default(c.Request.Method, c.ContentType())
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
		body, err = ioutil.ReadAll(c.Request.Body)
		if err != nil {
			return err
		}
		c.rawData = body
		c.Request.Body = ioutil.NopCloser(bytes.NewReader(body))
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

func (defaultBinding) Default(method, contentType string) Binding {
	if method == "GET" {
		return Form
	}

	switch contentType {
	case MIMEJSON:
		return JSON
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEPROTOBUF:
		return ProtoBuf
	case MIMEMSGPACK, MIMEMSGPACK2:
		return MsgPack
	case MIMEYAML:
		return YAML
	case MIMEMultipartPOSTForm:
		return FormMultipart
	default:
		return Form
	}
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
	return mapFormToStruct(req.Form, obj)
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

func (bindingProtoBuf) Bind(req *http.Request, obj interface{}) error {
	// 需要 protobuf 庫支援
	return fmt.Errorf("protobuf binding not implemented")
}

func (bindingProtoBuf) BindBody(body []byte, obj interface{}) error {
	// 需要 protobuf 庫支援
	return fmt.Errorf("protobuf binding not implemented")
}

// ===== MsgPack 綁定器 =====

type bindingMsgPack struct{}

func (bindingMsgPack) Name() string { return "msgpack" }

func (bindingMsgPack) Bind(req *http.Request, obj interface{}) error {
	// 需要 msgpack 庫支援
	return fmt.Errorf("msgpack binding not implemented")
}

func (bindingMsgPack) BindBody(body []byte, obj interface{}) error {
	// 需要 msgpack 庫支援
	return fmt.Errorf("msgpack binding not implemented")
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
	return mapFormToStruct(url.Values(req.Header), obj)
}

// ===== 輔助函數 =====

// decodeJSON 解碼 JSON
func decodeJSON(r io.Reader, obj interface{}) error {
	decoder := json.NewDecoder(r)
	return decoder.Decode(obj)
}

// mapFormToStruct 將表單映射到結構體（簡化版）
func mapFormToStruct(values url.Values, obj interface{}) error {
	// 這是一個簡化的實現
	// 實際專案中建議使用成熟的庫如 gorilla/schema 或 mapstructure

	// 使用 JSON 作為中間格式（簡化實現）
	data := make(map[string]interface{})
	for k, v := range values {
		if len(v) == 1 {
			data[k] = v[0]
		} else if len(v) > 1 {
			data[k] = v
		}
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonBytes, obj)
}
