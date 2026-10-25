package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/schema"
	"gopkg.in/yaml.v3"
)

// TypeFields 一個型別的 JSON 欄位（依宣告順序），在 manifest 的 types 區段以單行映射輸出：
// 欄位名 → 型別，必填欄位在型別後加 "!"，例如 `email: string!`、`age: integer`。
// 每個型別只列一次，路由以 input_type / output_type / response_types 引用名稱。
// 先前欄位內嵌在每條路由、每個欄位四行，同一型別被 N 條路由引用就重複 N 份
type TypeFields []schema.FieldInfo

// compactType 欄位的單行型別寫法
func compactType(f schema.FieldInfo) string {
	if f.Required {
		return f.Type + "!"
	}
	return f.Type
}

// parseCompactField 解析單行型別寫法
func parseCompactField(name, value string) schema.FieldInfo {
	typ, required := strings.CutSuffix(value, "!")
	return schema.FieldInfo{Name: name, Type: typ, Required: required}
}

// MarshalYAML 輸出成保留欄位順序的映射（Go map 會被依鍵排序）
func (f TypeFields) MarshalYAML() (interface{}, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, fi := range f {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fi.Name},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: compactType(fi)})
	}
	return n, nil
}

// UnmarshalYAML 讀回 MarshalYAML 的映射（hyp lint 等以 YAML 解析 manifest；JSON 也走這裡）
func (f *TypeFields) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("manifest: type fields must be a mapping, got %v", n.Tag)
	}
	*f = make(TypeFields, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		*f = append(*f, parseCompactField(n.Content[i].Value, n.Content[i+1].Value))
	}
	return nil
}

// MarshalJSON 輸出成保留欄位順序的 object
func (f TypeFields) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, fi := range f {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(fi.Name)
		v, _ := json.Marshal(compactType(fi))
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON 依出現順序讀回 object（hyp context 經 HYPGO_MANIFEST_OUT 取得 JSON）
func (f *TypeFields) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("manifest: type fields must be a JSON object")
	}
	*f = TypeFields{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		var value string
		if err := dec.Decode(&value); err != nil {
			return err
		}
		*f = append(*f, parseCompactField(tok.(string), value))
	}
	_, err := dec.Token() // 結尾的 '}'
	return err
}

// collectTypes 收集 schema 宣告的 Input / Output / Responses 型別欄位，以型別名稱為鍵。
// slice / map 型別登錄其元素型別：list 回應的 output_type 是 "[]UserResp"，types 要有 UserResp
// （先前 FieldsOf(slice) 為 nil，list 回應在 types 區段沒有任何定義）。
// ponytail: 以短型別名為鍵，不同套件的同名型別只留第一個（路由的 input_type 等本來就只有短名稱）
func collectTypes(types map[string]TypeFields, s *schema.Route) {
	add := func(name string, v interface{}) {
		if name == "" || v == nil {
			return
		}
		if t := elemType(reflect.TypeOf(v)); t != nil {
			v = reflect.Zero(t).Interface()
			name = schema.TypeName(v)
		}
		if _, ok := types[name]; ok {
			return
		}
		if fields := schema.FieldsOf(v); fields != nil {
			types[name] = fields
		}
	}
	add(s.InputName, s.Input)
	add(s.OutputName, s.Output)
	for _, resp := range s.Responses {
		add(resp.TypeName, resp.Type)
	}
}

// elemType 回傳 slice / array / map（可巢狀、可為指標）最內層的元素型別；t 不是這些型別時回 nil
func elemType(t reflect.Type) reflect.Type {
	unwrapped := false
	for {
		switch t.Kind() {
		case reflect.Ptr:
			t = t.Elem()
		case reflect.Slice, reflect.Array, reflect.Map:
			t, unwrapped = t.Elem(), true
		default:
			if unwrapped {
				return t
			}
			return nil
		}
	}
}
