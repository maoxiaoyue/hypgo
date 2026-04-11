// Package schema 提供 Schema-first 路由註冊系統
// 讓路由攜帶 metadata（輸入/輸出型別、描述、標籤），供 Manifest 生成與 Contract Testing 使用
package schema

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// ParamSchema 描述路由參數
type ParamSchema struct {
	Name     string `json:"name" yaml:"name"`
	In       string `json:"in" yaml:"in"`                                    // "path", "query", "header"
	Required bool   `json:"required" yaml:"required"`
	Type     string `json:"type" yaml:"type"`                                // "string", "int", "bool"
	Desc     string `json:"description,omitempty" yaml:"description,omitempty"`
}

// HeaderSchema 描述請求標頭
type HeaderSchema struct {
	Name     string `json:"name" yaml:"name"`
	Required bool   `json:"required" yaml:"required"`
	Desc     string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ResponseSchema 描述特定狀態碼的回應
type ResponseSchema struct {
	Description string      `json:"description" yaml:"description"`
	Type        interface{} `json:"-" yaml:"-"`                                   // Go struct type（用於驗證）
	TypeName    string      `json:"type_name,omitempty" yaml:"type_name,omitempty"`
}

// Route 描述一個帶 metadata 的路由（支援多協議）
//
// REST 使用範例：
//
//	router.Schema(schema.Route{
//	    Method:  "POST",
//	    Path:    "/api/users",
//	    Summary: "建立使用者",
//	    Input:   CreateUserRequest{},
//	    Output:  UserResponse{},
//	}).Handle(createUserHandler)
//
// 非 REST 使用範例：
//
//	schema.RegisterGRPC("UserService/CreateUser", "Create user", Input{}, Output{})
//	schema.RegisterBot("/start", "Start the bot", nil, WelcomeMsg{})
//	schema.RegisterMCP("search_repos", "Search repos", Input{}, Output{})
type Route struct {
	// 通用欄位（所有協議共用）
	Protocol    string                 `json:"protocol,omitempty" yaml:"protocol,omitempty"`       // "rest"(default), "grpc", "bot", "mcp", "websocket", "cli"
	Command     string                 `json:"command,omitempty" yaml:"command,omitempty"`         // 非 REST 的命令標識
	Platform    string                 `json:"platform,omitempty" yaml:"platform,omitempty"`       // bot 專用平台："telegram", "line", "discord", "whatsapp"
	Summary     string                 `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty" yaml:"tags,omitempty"`
	Input       interface{}            `json:"-" yaml:"-"`
	Output      interface{}            `json:"-" yaml:"-"`
	InputName   string                 `json:"input_type,omitempty" yaml:"input_type,omitempty"`
	OutputName  string                 `json:"output_type,omitempty" yaml:"output_type,omitempty"`

	// REST 專用（向後相容）
	Method    string                 `json:"method,omitempty" yaml:"method,omitempty"`
	Path      string                 `json:"path,omitempty" yaml:"path,omitempty"`
	Params    []ParamSchema          `json:"params,omitempty" yaml:"params,omitempty"`
	Headers   []HeaderSchema         `json:"headers,omitempty" yaml:"headers,omitempty"`
	Responses map[int]ResponseSchema `json:"responses,omitempty" yaml:"responses,omitempty"`
}

// RouteKey 回傳 Registry 的查詢 key
func (r Route) RouteKey() string {
	proto := r.Protocol
	if proto == "" {
		proto = "rest"
	}
	if proto == "rest" {
		return proto + "|" + r.Method + " " + r.Path
	}
	return proto + "|" + r.Command
}

// IsREST 判斷是否為 REST 路由
func (r Route) IsREST() bool {
	return r.Protocol == "" || r.Protocol == "rest"
}

// SchemaRegistrar 由 Router 和 Group 實作，用於將 schema 與路由一起註冊
type SchemaRegistrar interface {
	RegisterSchema(route Route, handlers ...hypcontext.HandlerFunc)
}

// SchemaRoute 是 builder pattern 的中間物件
// 呼叫 Handle() 完成路由 + schema 的註冊
type SchemaRoute struct {
	route      Route
	registrar  SchemaRegistrar
}

// NewSchemaRoute 建立新的 SchemaRoute builder
func NewSchemaRoute(route Route, registrar SchemaRegistrar) *SchemaRoute {
	// 自動填入型別名稱
	if route.Input != nil && route.InputName == "" {
		route.InputName = TypeName(route.Input)
	}
	if route.Output != nil && route.OutputName == "" {
		route.OutputName = TypeName(route.Output)
	}

	// 自動填入 ResponseSchema 的 TypeName
	for code, resp := range route.Responses {
		if resp.Type != nil && resp.TypeName == "" {
			resp.TypeName = TypeName(resp.Type)
			route.Responses[code] = resp
		}
	}

	return &SchemaRoute{
		route:     route,
		registrar: registrar,
	}
}

// Handle 完成路由註冊並儲存 schema
func (sr *SchemaRoute) Handle(handlers ...hypcontext.HandlerFunc) {
	sr.registrar.RegisterSchema(sr.route, handlers...)
}
