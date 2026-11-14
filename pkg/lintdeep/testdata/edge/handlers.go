// Package edge 是 lintdeep 的迴歸測試夾具：路由寫法與 handler 綁定的邊界案例。
// 預期結果在路由註解與 regress_test.go。
package edge

import (
	stdcontext "context"
	"net/http"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type Req struct{ A string }
type Other struct{ B string }
type AliasReq = Req
type PtrAlias = *Req

const prefix = "/const"

// ── handler ──

func noBind(c *hypcontext.Context)    { c.String(200, "x") }
func wrongBind(c *hypcontext.Context) { var o Other; c.BindInput(&o) }
func rightBind(c *hypcontext.Context) { var r Req; c.BindInput(&r) }

func viaHelper(c *hypcontext.Context)      { helper(c) }
func viaHelperWrong(c *hypcontext.Context) { helperWrong(c) }
func viaChain(c *hypcontext.Context)       { chain1(c) }
func viaGeneric(c *hypcontext.Context)     { bindT[Req](c) }
func viaAlias(c *hypcontext.Context)       { var r AliasReq; c.BindInput(&r) }
func viaPtrAlias(c *hypcontext.Context)    { var p PtrAlias = &Req{}; c.BindInput(p) }
func viaMap(c *hypcontext.Context)         { var m map[string]any; c.BindInput(&m) }
func viaSlice(c *hypcontext.Context)       { var s []Req; c.BindInput(&s) }
func viaLoop(c *hypcontext.Context)        { loopA(c) }
func viaHandlerArg(c *hypcontext.Context)  { run(c, rightBind) }
func viaMethodValue(c *hypcontext.Context) { bind := c.BindInput; var r Req; bind(&r) }

func helper(c *hypcontext.Context) bool      { var r Req; return c.BindInput(&r) }
func helperWrong(c *hypcontext.Context) bool { var o Other; return c.BindInput(&o) }
func chain1(c *hypcontext.Context)           { chain2(c) }
func chain2(c *hypcontext.Context)           { helper(c) }
func loopA(c *hypcontext.Context)            { loopB(c) }
func loopB(c *hypcontext.Context)            { loopA(c) }
func run(c *hypcontext.Context, h hypcontext.HandlerFunc) {
	h(c)
}

func bindT[T any](c *hypcontext.Context) (T, bool) {
	var v T
	ok := c.BindInput(&v)
	return v, ok
}

// Service 介面方法追不到本體
type Service interface {
	Handle(c *hypcontext.Context)
	Save(ctx stdcontext.Context) error
}

type Ctrl struct{ svc Service }

func (h Ctrl) viaInterface(c *hypcontext.Context) { h.svc.Handle(c) }

// c 當 context.Context 傳出去不可能在裡面綁定，仍然要回報 no_bindinput
func (h Ctrl) passesAsStdContext(c *hypcontext.Context) { _ = h.svc.Save(c) }

// Gen 泛型 receiver
type Gen[T any] struct{}

func (Gen[T]) Wrong(c *hypcontext.Context) { var o Other; c.BindInput(&o) }
func (Gen[T]) ByT(c *hypcontext.Context)   { var v []T; c.BindInput(&v) }

func factory() hypcontext.HandlerFunc { return rightBind }

// ── 路由 ──

// Register 每條路由的預期結果見註解
func Register(r *router.Router, ctrl Ctrl, parent *router.Group) {
	// 常數 Method 與 Path
	r.Schema(schema.Route{Method: http.MethodPost, Path: "/const-method", Input: Req{}}).Handle(noBind)      // no_bindinput
	r.Schema(schema.Route{Method: http.MethodPost, Path: prefix + "/wrong", Input: Req{}}).Handle(wrongBind) // mismatch
	r.Schema(schema.Route{Method: "post", Path: "/lower", Input: Req{}}).Handle(noBind)                      // no_bindinput
	method := "POST"
	r.Schema(schema.Route{Method: method, Path: "/var-method", Input: Req{}}).Handle(noBind) // skipped
	path := "/var-path"
	r.Schema(schema.Route{Method: "POST", Path: path, Input: Req{}}).Handle(noBind) // no_bindinput，路徑顯示為 dynamic

	// 不是 r.Schema(<literal>).Handle(h) 的註冊方式：全部略過，不能看不到
	rt := schema.Route{Method: "POST", Path: "/var-route", Input: Req{}}
	r.Schema(rt).Handle(wrongBind)
	for _, x := range []schema.Route{{Method: "POST", Path: "/table", Input: Req{}}} {
		r.Schema(x).Handle(wrongBind)
	}
	sr := r.Schema(schema.Route{Method: "POST", Path: "/split", Input: Req{}})
	sr.Handle(wrongBind)
	r.RegisterSchema(rt, wrongBind)
	// RegisterSchema 字面值可以比對
	r.RegisterSchema(schema.Route{Method: "POST", Path: "/registered", Input: Req{}}, wrongBind) // mismatch

	// helper：不該誤報
	r.Schema(schema.Route{Method: "POST", Path: "/helper", Input: Req{}}).Handle(viaHelper)
	r.Schema(schema.Route{Method: "POST", Path: "/chain", Input: Req{}}).Handle(viaChain)
	r.Schema(schema.Route{Method: "POST", Path: "/handler-arg", Input: Req{}}).Handle(viaHandlerArg)
	r.Schema(schema.Route{Method: "POST", Path: "/helper-wrong", Input: Req{}}).Handle(viaHelperWrong)         // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/loop", Input: Req{}}).Handle(viaLoop)                        // no_bindinput（循環呼叫不能卡住）
	r.Schema(schema.Route{Method: "POST", Path: "/generic-helper", Input: Req{}}).Handle(viaGeneric)           // skipped
	r.Schema(schema.Route{Method: "POST", Path: "/interface", Input: Req{}}).Handle(ctrl.viaInterface)         // skipped
	r.Schema(schema.Route{Method: "POST", Path: "/method-value", Input: Req{}}).Handle(viaMethodValue)         // skipped
	r.Schema(schema.Route{Method: "POST", Path: "/std-context", Input: Req{}}).Handle(ctrl.passesAsStdContext) // no_bindinput
	r.Schema(schema.Route{Method: "GET", Path: "/get-unknown", Input: Req{}}).Handle(ctrl.viaInterface)        // 不可寫方法沒有綁定不算問題

	// 型別
	r.Schema(schema.Route{Method: "POST", Path: "/alias", Input: Req{}}).Handle(viaAlias)
	r.Schema(schema.Route{Method: "POST", Path: "/ptr-alias", Input: Req{}}).Handle(viaPtrAlias)
	r.Schema(schema.Route{Method: "POST", Path: "/alias-input", Input: AliasReq{}}).Handle(rightBind)
	r.Schema(schema.Route{Method: "POST", Path: "/pointer-input", Input: &Req{}}).Handle(rightBind)
	r.Schema(schema.Route{Method: "POST", Path: "/slice", Input: []Req{}}).Handle(viaSlice)
	r.Schema(schema.Route{Method: "POST", Path: "/map", Input: Req{}}).Handle(viaMap) // mismatch，不是「未呼叫」
	var anyInput any = Req{}
	r.Schema(schema.Route{Method: "POST", Path: "/any-input", Input: anyInput}).Handle(rightBind) // skipped
	r.Schema(schema.Route{Method: "POST", Path: "/nil-input", Input: nil}).Handle(noBind)         // 交給 shallow

	// 泛型 receiver
	var gen Gen[int]
	r.Schema(schema.Route{Method: "POST", Path: "/gen-wrong", Input: Req{}}).Handle(gen.Wrong) // mismatch
	r.Schema(schema.Route{Method: "POST", Path: "/gen-t", Input: Req{}}).Handle(gen.ByT)       // skipped

	// handler 無法定位
	r.Schema(schema.Route{Method: "POST", Path: "/factory", Input: Req{}}).Handle(factory()) // skipped

	// Group 前綴
	api := r.NewGroup("/api/v1")
	api.Schema(schema.Route{Method: "POST", Path: "/grp", Input: Req{}}).Handle(wrongBind) // POST /api/v1/grp
	admin := api.NewGroup("/admin")
	admin.Schema(schema.Route{Method: "POST", Path: "/nested", Input: Req{}}).Handle(wrongBind)            // POST /api/v1/admin/nested
	r.NewGroup("/inline").Schema(schema.Route{Method: "POST", Path: "/x", Input: Req{}}).Handle(wrongBind) // POST /inline/x
	parent.Schema(schema.Route{Method: "POST", Path: "/param-group", Input: Req{}}).Handle(wrongBind)      // 解析不到，維持相對路徑
	moved := r.NewGroup("/first")
	moved = r.NewGroup("/second")
	moved.Schema(schema.Route{Method: "POST", Path: "/reassigned", Input: Req{}}).Handle(wrongBind) // 值不唯一，維持相對路徑
}
