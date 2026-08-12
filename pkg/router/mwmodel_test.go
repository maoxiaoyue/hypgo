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
