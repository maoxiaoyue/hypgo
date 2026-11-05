// Package broken 是 lintdeep 的測試夾具：刻意編譯不過，但路由綁定正確。
package broken

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

type Req struct{}

func handler(c *hypcontext.Context) {
	var r Req
	c.BindInput(&r)
	undefinedThing()
}

// Register 註冊一條綁定正確的路由
func Register(r *router.Router) {
	r.Schema(schema.Route{Method: "POST", Path: "/b", Input: Req{}}).Handle(handler)
}
