package contract

import "testing"

type omitzeroResp struct {
	A string `json:"a,omitzero"`
	B int    `json:"b"`
}

// TestOmitzeroFieldIsOptional 回歸測試：omitzero（Go 1.24）欄位零值時不會序列化，不能算必填。
func TestOmitzeroFieldIsOptional(t *testing.T) {
	if err := validateResponse([]byte(`{"b":1}`), omitzeroResp{}); err != nil {
		t.Errorf("validateResponse: %v", err)
	}
}
