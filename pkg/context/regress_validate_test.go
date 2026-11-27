package context_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
)

type typoReq struct {
	Name string `json:"name" validate:"requried"`
}

// 先前 validate 標籤打錯時，這條路由的每個請求都 panic（靠 Recovery 接住）。
// 這是伺服器端的錯：回 500、不洩漏細節，並把原因交給 server 記錄
func TestBindInputWithInvalidValidateTag(t *testing.T) {
	r := router.New()
	var recorded []string
	r.POST("/t", func(c *hypcontext.Context) {
		var v typoReq
		ok := c.BindInput(&v)
		for _, e := range c.Errors {
			recorded = append(recorded, e.Error())
		}
		if ok {
			c.JSON(200, v)
		}
	})
	req := httptest.NewRequest("POST", "/t", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/json")
	w := serve(r, req) // panic 會直接讓測試失敗
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"E0003"`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "requried") {
		t.Errorf("the response leaks the tag: %s", w.Body.String())
	}
	if len(recorded) != 1 || !strings.Contains(recorded[0], "requried") {
		t.Errorf("the cause should be recorded for the server log: %v", recorded)
	}
}

type queryReq struct {
	Q     string `form:"q" validate:"required"`
	Limit int    `form:"limit" validate:"min=1"`
}

// 先前 query 綁定的驗證錯誤以 Go 欄位名為 key（Q、Limit），客戶端對不上自己送的參數
func TestQueryValidationErrorsUseParameterNames(t *testing.T) {
	r := router.New()
	r.GET("/q", func(c *hypcontext.Context) {
		var v queryReq
		if c.BindInput(&v) {
			c.JSON(200, v)
		}
	})
	w := serve(r, httptest.NewRequest("GET", "/q?limit=0", nil))
	var body struct {
		Details map[string]string `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 422 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if body.Details["q"] != "q is required" || body.Details["limit"] != "limit must be at least 1" || len(body.Details) != 2 {
		t.Errorf("details = %v", body.Details)
	}
}
