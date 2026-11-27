package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

type probItem struct {
	ID int `json:"id"`
}

// probNullStr 模擬 uuid.NullUUID / null.String：沒有值時序列化成 null
type probNullStr struct {
	S     string
	Valid bool
}

func (n probNullStr) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.S)
}

func (n *probNullStr) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*n = probNullStr{}
		return nil
	}
	n.Valid = true
	return json.Unmarshal(b, &n.S)
}

// 宣告 slice / map 的回應是 null 時通過：nil slice 序列化成 null，是空資料表的正常結果。
// 歷史 bug：一律 "response body is null"，回傳空清單的端點過不了 contract.TestAll
func TestNullBodyForNullableOutput(t *testing.T) {
	for name, typ := range map[string]interface{}{"slice": []probItem{}, "map": map[string]probItem{}} {
		if err := validateBody([]byte("null"), typ, "response", "Output", false); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// struct 的回應是 null 仍然失敗
	if err := validateBody([]byte("null"), probItem{}, "response", "Output", false); err == nil || !strings.Contains(err.Error(), "body is null") {
		t.Errorf("struct output: %v", err)
	}
}

// 自訂序列化的必填欄位是 null 時通過。歷史 bug：uuid.NullUUID / null.String 這類型別被報 "is null"
func TestNullForCustomMarshalledField(t *testing.T) {
	type out struct {
		ID   int         `json:"id"`
		Nick probNullStr `json:"nick"`
	}
	if err := validateBody([]byte(`{"id":1,"nick":null}`), out{}, "response", "Output", false); err != nil {
		t.Errorf("custom null: %v", err)
	}
	// 一般的必填欄位照舊
	if err := validateBody([]byte(`{"id":null,"nick":"a"}`), out{}, "response", "Output", false); err == nil || !strings.Contains(err.Error(), "id is null") {
		t.Errorf("plain required field: %v", err)
	}
}

// 沒有名稱的案例以 body 加 query 辨識。歷史 bug：只算 body，同一條路由上只差在 query 的案例
// 被 hyp eval-report 當成同一個
func TestCaseHashIncludesQuery(t *testing.T) {
	plain := caseHash(TestCase{Input: `{"a":1}`})
	if plain != "sha256:015abd7f5cc57a2d" {
		t.Errorf("hash without a query changed (%s): existing history would no longer line up", plain)
	}
	active := caseHash(TestCase{Input: `{"a":1}`, Query: map[string]string{"status": "active", "page": "1"}})
	banned := caseHash(TestCase{Input: `{"a":1}`, Query: map[string]string{"status": "banned", "page": "1"}})
	again := caseHash(TestCase{Input: `{"a":1}`, Query: map[string]string{"page": "1", "status": "active"}})
	if active == plain || active == banned || active != again {
		t.Errorf("plain=%s active=%s banned=%s again=%s", plain, active, banned, again)
	}
	// 標頭不算（測試裡的 token 每次都不同）
	if caseHash(TestCase{Input: `{"a":1}`, Headers: map[string]string{"Authorization": "Bearer x"}}) != plain {
		t.Error("headers changed the case identity")
	}
	if caseHash(TestCase{Query: map[string]string{"q": "x"}}) == "" {
		t.Error("a query-only case has no identity")
	}
}
