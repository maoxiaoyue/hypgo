package scaffold

// controllerTemplate 使用 HypGo 原生 API（Schema-first 路由 + Error Catalog）
const controllerTemplate = `package controllers

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/errors"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// {{.Name}}Controller handles {{.LowerName}} CRUD operations
type {{.Name}}Controller struct{}

// 預定義錯誤碼
var (
	Err{{.Name}}NotFound = errors.Define("E_{{.LowerName}}_001", 404, "{{.Name}} not found", "{{.LowerName}}")
	Err{{.Name}}Invalid  = errors.Define("E_{{.LowerName}}_002", 400, "Invalid {{.LowerName}} data", "{{.LowerName}}")
)

// RegisterRoutes 註冊 {{.Name}} 相關路由（使用 Schema-first）
func (ctrl *{{.Name}}Controller) RegisterRoutes(r *router.Router) {
	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/{{.LowerName}}",
		Summary: "List all {{.LowerName}}s",
		Tags:    []string{"{{.LowerName}}"},
	}).Handle(ctrl.List)

	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/{{.LowerName}}",
		Summary: "Create {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "{{.Name}} created"},
		},
	}).Handle(ctrl.Create)

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Get {{.LowerName}} by ID",
		Tags:    []string{"{{.LowerName}}"},
	}).Handle(ctrl.Get)

	r.Schema(schema.Route{
		Method:  "PUT",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Update {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
	}).Handle(ctrl.Update)

	r.Schema(schema.Route{
		Method:  "DELETE",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Delete {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
		Responses: map[int]schema.ResponseSchema{
			204: {Description: "{{.Name}} deleted"},
		},
	}).Handle(ctrl.Delete)
}

func (ctrl *{{.Name}}Controller) List(c *hypcontext.Context) {
	c.JSON(200, map[string]interface{}{
		"data": []interface{}{},
	})
}

func (ctrl *{{.Name}}Controller) Create(c *hypcontext.Context) {
	// TODO: Parse and validate input
	c.JSON(201, map[string]interface{}{
		"message": "{{.Name}} created",
	})
}

func (ctrl *{{.Name}}Controller) Get(c *hypcontext.Context) {
	id := c.Param("id")
	if id == "" {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", "missing id"))
		return
	}

	// TODO: Fetch from database
	c.JSON(200, map[string]interface{}{
		"id": id,
	})
}

func (ctrl *{{.Name}}Controller) Update(c *hypcontext.Context) {
	id := c.Param("id")
	if id == "" {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", "missing id"))
		return
	}

	// TODO: Parse input and update
	c.JSON(200, map[string]interface{}{
		"id":      id,
		"message": "{{.Name}} updated",
	})
}

func (ctrl *{{.Name}}Controller) Delete(c *hypcontext.Context) {
	id := c.Param("id")
	if id == "" {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", "missing id"))
		return
	}

	// TODO: Delete from database
	c.Status(204)
	c.Writer.WriteHeaderNow()
}
`

// modelTemplate 使用 bun ORM 格式
const modelTemplate = `package models

import (
	"time"

	"github.com/uptrace/bun"
)

// {{.Name}} 資料模型
type {{.Name}} struct {
	bun.BaseModel ` + "`" + `bun:"table:{{.TableName}},alias:{{.Alias}}"` + "`" + `

	ID          int64     ` + "`" + `bun:"id,pk,autoincrement" json:"id"` + "`" + `
	Name        string    ` + "`" + `bun:"name,notnull" json:"name"` + "`" + `
	Description string    ` + "`" + `bun:"description" json:"description,omitempty"` + "`" + `
	Active      bool      ` + "`" + `bun:"active,notnull,default:true" json:"active"` + "`" + `
	CreatedAt   time.Time ` + "`" + `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"` + "`" + `
	UpdatedAt   time.Time ` + "`" + `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"` + "`" + `
}
`

// serviceTemplate 使用 Error Catalog
const serviceTemplate = `package services

import (
	"context"

	"github.com/maoxiaoyue/hypgo/pkg/errors"
	"github.com/maoxiaoyue/hypgo/pkg/hidb"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// {{.Name}}Service 處理 {{.LowerName}} 業務邏輯
type {{.Name}}Service struct {
	db     *hidb.Database
	logger *logger.Logger
}

// 預定義錯誤
var (
	ErrSvc{{.Name}}NotFound = errors.Define("E_svc_{{.LowerName}}_001", 404, "{{.Name}} not found", "{{.LowerName}}")
	ErrSvc{{.Name}}Create   = errors.Define("E_svc_{{.LowerName}}_002", 500, "Failed to create {{.LowerName}}", "{{.LowerName}}")
)

func New{{.Name}}Service(db *hidb.Database, logger *logger.Logger) *{{.Name}}Service {
	return &{{.Name}}Service{db: db, logger: logger}
}

func (s *{{.Name}}Service) Create(ctx context.Context, data map[string]interface{}) error {
	s.logger.Info("Creating {{.LowerName}}")
	// TODO: implement
	return nil
}

func (s *{{.Name}}Service) GetByID(ctx context.Context, id string) (interface{}, error) {
	s.logger.Info("Getting {{.LowerName}} by ID: %s", id)
	// TODO: implement
	return nil, ErrSvc{{.Name}}NotFound.With("id", id)
}

func (s *{{.Name}}Service) Update(ctx context.Context, id string, data map[string]interface{}) error {
	s.logger.Info("Updating {{.LowerName}} ID: %s", id)
	// TODO: implement
	return nil
}

func (s *{{.Name}}Service) Delete(ctx context.Context, id string) error {
	s.logger.Info("Deleting {{.LowerName}} ID: %s", id)
	// TODO: implement
	return nil
}

func (s *{{.Name}}Service) List(ctx context.Context) ([]interface{}, error) {
	s.logger.Info("Listing {{.LowerName}}s")
	// TODO: implement
	return nil, nil
}
`
