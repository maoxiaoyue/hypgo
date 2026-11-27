// Package second 是 lintdeep 第二輪的迴歸測試夾具，預期結果見路由註解與 problems_test.go。
package second

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type Req struct{ A string }
type Other struct{ B string }

type Svc interface{ Do(c *hypcontext.Context) }

var after = func(c *hypcontext.Context) {}

func run(c *hypcontext.Context, f func(*hypcontext.Context)) { f(c) }

func noBind(c *hypcontext.Context) { c.String(200, "x") }

// 明確綁錯型別，另有追不到的呼叫
func wrongThenFuncVar(c *hypcontext.Context) {
	var o Other
	c.BindInput(&o)
	run(c, swapped)
}

type Ctrl struct{ svc Svc }

func (h Ctrl) wrongThenInterface(c *hypcontext.Context) {
	var o Other
	c.BindInput(&o)
	h.svc.Do(c)
}

func (h Ctrl) viaSvc(c *hypcontext.Context) { h.svc.Do(c) }

// 明確綁錯型別，另有一個型別不確定的 BindInput：對的可能是那一個
func wrongPlusAnyBind(c *hypcontext.Context) {
	var o Other
	c.BindInput(&o)
	var v any = &Req{}
	c.BindInput(v)
}

// 綁定寫在前面的 handler
func bindFirst(c *hypcontext.Context) {
	var r Req
	if !c.BindInput(&r) {
		return
	}
	c.Next()
}

func bindFirstWrong(c *hypcontext.Context) {
	var o Other
	c.BindInput(&o)
	c.Next()
}

func factory() hypcontext.HandlerFunc { return noBind }

// 套件層級的函式變數
var varRight = func(c *hypcontext.Context) { var r Req; c.BindInput(&r) }
var varWrong = func(c *hypcontext.Context) { var o Other; c.BindInput(&o) }
var swapped = func(c *hypcontext.Context) { var r Req; c.BindInput(&r) }

func init() { swapped = noBind }

// Register 每條路由的預期結果見註解
func Register(r *router.Router, ctrl Ctrl) {
	r.Schema(schema.Route{Method: "POST", Path: "/wrong-func-var", Input: Req{}}).Handle(wrongThenFuncVar)         // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/wrong-interface", Input: Req{}}).Handle(ctrl.wrongThenInterface) // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/wrong-any-bind", Input: Req{}}).Handle(wrongPlusAnyBind)         // skipped

	r.Schema(schema.Route{Method: "POST", Path: "/mw-binds", Input: Req{}}).Handle(bindFirst, noBind)             // 通過
	r.Schema(schema.Route{Method: "POST", Path: "/mw-binds-wrong", Input: Req{}}).Handle(bindFirstWrong, noBind)  // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/mw-factory", Input: Req{}}).Handle(factory(), noBind)           // no_bindinput
	r.Schema(schema.Route{Method: "POST", Path: "/mw-factory-binds", Input: Req{}}).Handle(factory(), varRight)   // 通過
	r.Schema(schema.Route{Method: "POST", Path: "/last-factory", Input: Req{}}).Handle(bindFirstWrong, factory()) // skipped

	r.Schema(schema.Route{Method: "POST", Path: "/var-right", Input: Req{}}).Handle(varRight)  // 通過
	r.Schema(schema.Route{Method: "POST", Path: "/var-wrong", Input: Req{}}).Handle(varWrong)  // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/var-swapped", Input: Req{}}).Handle(swapped) // skipped：被重新賦值

	r.Schema(schema.Route{Method: "GET", Path: "/get-unbound", Input: Req{}}).Handle(noBind)      // info
	r.Schema(schema.Route{Method: "DELETE", Path: "/del-unbound", Input: Req{}}).Handle(noBind)   // info
	r.Schema(schema.Route{Method: "GET", Path: "/get-bound", Input: Req{}}).Handle(varRight)      // 通過
	r.Schema(schema.Route{Method: "GET", Path: "/get-wrong", Input: Req{}}).Handle(varWrong)      // mismatch
	r.Schema(schema.Route{Method: "GET", Path: "/get-unknown", Input: Req{}}).Handle(ctrl.viaSvc) // 不列
}
