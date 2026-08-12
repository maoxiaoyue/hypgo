package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// TestMiddlewareWrapSemantics 回歸測試：中間件執行模型必須是 gin 式洋蔥模型。
// 歷史 bug：executeHandlers 攤平循序執行、c.handlers 從未填入，中間件內的
// c.Next() 是 no-op——「後半段」在 handler 之前就執行（順序 before→after→handler）。
func TestMiddlewareWrapSemantics(t *testing.T) {
	var order []string

	r := New()
	r.Use(func(c *hypcontext.Context) {
		order = append(order, "before")
		c.Next()
		order = append(order, "after")
	})
	r.GET("/x", func(c *hypcontext.Context) {
		order = append(order, "handler")
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))

	if got := strings.Join(order, ","); got != "before,handler,after" {
		t.Errorf("execution order = %s, want before,handler,after (onion model)", got)
	}
}

// TestRecoveryStyleMiddlewareCatchesPanic 回歸測試：gin 風格 recovery 中間件
// 必須接得住 handler 的 panic。歷史 bug：攤平模型下 handler 在 recovery 的
// stack frame 之外執行，panic 直接穿透——Recovery 中間件形同虛設。
func TestRecoveryStyleMiddlewareCatchesPanic(t *testing.T) {
	r := New()
	r.Use(func(c *hypcontext.Context) {
		defer func() {
			if err := recover(); err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	})
	r.GET("/boom", func(c *hypcontext.Context) {
		panic("kaboom")
	})

	defer func() {
		if err := recover(); err != nil {
			t.Fatalf("panic escaped the recovery middleware: %v", err)
		}
	}()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (recovery middleware should convert panic)", w.Code)
	}
}

// TestLongMiddlewareChain 回歸測試：handler 鏈長超過舊 abortIndex(63) 仍須正確執行。
// 歷史 bug：Context.index 為 int8、abortIndex = MaxInt8/2 = 63，一旦
// 「全域中間件 + 路由 handler」總數超過 63：
//   - 64~127 個 → c.index++ 溢位成 -128 → index out of range [-128] panic
//   - 128 個以上 → 溢位後 index >= abortIndex 恆成立，整條鏈一個都不執行，
//     卻靜默回 200 空回應（最陰險的失敗模式）
//
// 修法：index 改 int32、abortIndex 改 MaxInt32/2。
func TestLongMiddlewareChain(t *testing.T) {
	for _, n := range []int{63, 64, 130} {
		ran := 0
		handlerRan := false

		r := New()
		for i := 0; i < n; i++ {
			r.Use(func(c *hypcontext.Context) { ran++; c.Next() })
		}
		r.GET("/x", func(c *hypcontext.Context) {
			handlerRan = true
			c.String(200, "ok")
		})

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))

		if ran != n {
			t.Errorf("chain of %d middleware: ran=%d, want %d", n, ran, n)
		}
		if !handlerRan {
			t.Errorf("chain of %d middleware: handler never ran", n)
		}
		if w.Body.String() != "ok" {
			t.Errorf("chain of %d middleware: body=%q, want \"ok\"", n, w.Body.String())
		}
	}
}

// TestAbortStopsChain c.Abort* 必須中止後續 handler
func TestAbortStopsChain(t *testing.T) {
	handlerRan := false

	r := New()
	r.Use(func(c *hypcontext.Context) {
		c.AbortWithStatus(http.StatusUnauthorized) // 模擬 Auth 擋下
	})
	r.GET("/secret", func(c *hypcontext.Context) {
		handlerRan = true
		c.String(200, "secret")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/secret", nil))

	if handlerRan {
		t.Error("handler ran after middleware aborted the chain")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
