package scaffold

// controllerTemplate — 只放 handler 邏輯，路由定義移至 routers/
const controllerTemplate = `package controllers

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/errors"
	"{{.ModuleName}}/app/models"
)

// {{.Name}}Controller handles {{.LowerName}} CRUD operations
type {{.Name}}Controller struct{}

// 預定義錯誤碼
var (
	Err{{.Name}}NotFound = errors.Define("E_{{.LowerName}}_001", 404, "{{.Name}} not found", "{{.LowerName}}")
	Err{{.Name}}Invalid  = errors.Define("E_{{.LowerName}}_002", 400, "Invalid {{.LowerName}} data", "{{.LowerName}}")
)

func (ctrl *{{.Name}}Controller) List(c *hypcontext.Context) {
	// TODO: Fetch from database
	c.JSON(200, models.{{.Name}}ListResp{
		Data:  []models.{{.Name}}Resp{},
		Total: 0,
	})
}

func (ctrl *{{.Name}}Controller) Create(c *hypcontext.Context) {
	var req models.Create{{.Name}}Req
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", err.Error()))
		return
	}

	// TODO: Save to database
	c.JSON(201, models.{{.Name}}Resp{
		ID:   1,
		Name: req.Name,
	})
}

func (ctrl *{{.Name}}Controller) Get(c *hypcontext.Context) {
	id := c.Param("id")
	if id == "" {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", "missing id"))
		return
	}

	// TODO: Fetch from database
	c.JSON(200, models.{{.Name}}Resp{
		ID: 1,
	})
}

func (ctrl *{{.Name}}Controller) Update(c *hypcontext.Context) {
	id := c.Param("id")
	if id == "" {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", "missing id"))
		return
	}

	var req models.Update{{.Name}}Req
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.AbortWithAppError(c, Err{{.Name}}Invalid.With("reason", err.Error()))
		return
	}

	// TODO: Update in database
	c.JSON(200, models.{{.Name}}Resp{
		ID: 1,
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

// routerTemplate — 單一資源的 Schema-first 路由定義
const routerTemplate = `package routers

import (
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
	"{{.ModuleName}}/app/controllers"
	"{{.ModuleName}}/app/models"
)

// Register{{.Name}}Routes 註冊 {{.Name}} 相關路由（Schema-first）
func Register{{.Name}}Routes(r *router.Router) {
	ctrl := &controllers.{{.Name}}Controller{}

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/{{.LowerName}}",
		Summary: "List all {{.LowerName}}s",
		Tags:    []string{"{{.LowerName}}"},
		Output:  models.{{.Name}}ListResp{},
	}).Handle(ctrl.List)

	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/{{.LowerName}}",
		Summary: "Create {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
		Input:   models.Create{{.Name}}Req{},
		Output:  models.{{.Name}}Resp{},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "{{.Name}} created"},
			400: {Description: "Invalid input"},
		},
	}).Handle(ctrl.Create)

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Get {{.LowerName}} by ID",
		Tags:    []string{"{{.LowerName}}"},
		Output:  models.{{.Name}}Resp{},
		Responses: map[int]schema.ResponseSchema{
			200: {Description: "{{.Name}} found"},
			404: {Description: "{{.Name}} not found"},
		},
	}).Handle(ctrl.Get)

	r.Schema(schema.Route{
		Method:  "PUT",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Update {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
		Input:   models.Update{{.Name}}Req{},
		Output:  models.{{.Name}}Resp{},
		Responses: map[int]schema.ResponseSchema{
			200: {Description: "{{.Name}} updated"},
			400: {Description: "Invalid input"},
			404: {Description: "{{.Name}} not found"},
		},
	}).Handle(ctrl.Update)

	r.Schema(schema.Route{
		Method:  "DELETE",
		Path:    "/api/{{.LowerName}}/:id",
		Summary: "Delete {{.LowerName}}",
		Tags:    []string{"{{.LowerName}}"},
		Responses: map[int]schema.ResponseSchema{
			204: {Description: "{{.Name}} deleted"},
			404: {Description: "{{.Name}} not found"},
		},
	}).Handle(ctrl.Delete)
}
`

// routerSetupTemplate — routers/router.go 總入口（只在首次生成）
const routerSetupTemplate = `package routers

import (
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
)

// Setup 設定所有路由和中間件
// 在 main.go 中呼叫：routers.Setup(srv.Router())
func Setup(r *router.Router) {
	// 全域中間件
	r.Use(middleware.DefaultMiddleware()...)

	// 在此註冊各資源的路由
	// Register{{.Name}}Routes(r)
}
`

// middlewareTemplate — routers/middleware.go 中間件配置
const middlewareTemplate = `package routers

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
)

// APIMiddleware 回傳 API 路由群組專用的中間件鏈
func APIMiddleware() []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		middleware.Logger(middleware.LoggerConfig{}),
		// middleware.JWT(middleware.JWTConfig{...}),
		// middleware.RateLimit(100),
	}
}

// WebMiddleware 回傳 Web 頁面路由群組專用的中間件鏈
func WebMiddleware() []hypcontext.HandlerFunc {
	return []hypcontext.HandlerFunc{
		middleware.Logger(middleware.LoggerConfig{}),
		// middleware.CSRF(middleware.CSRFConfig{...}),
	}
}
`

// modelTemplate — bun ORM 模型 + Request/Response struct
const modelTemplate = `package models

import (
	"time"

	"github.com/uptrace/bun"
)

// {{.Name}} 資料模型（DB schema）
type {{.Name}} struct {
	bun.BaseModel ` + "`" + `bun:"table:{{.TableName}},alias:{{.Alias}}"` + "`" + `

	ID          int64     ` + "`" + `bun:"id,pk,autoincrement" json:"id"` + "`" + `
	Name        string    ` + "`" + `bun:"name,notnull" json:"name"` + "`" + `
	Description string    ` + "`" + `bun:"description" json:"description,omitempty"` + "`" + `
	Active      bool      ` + "`" + `bun:"active,notnull,default:true" json:"active"` + "`" + `
	CreatedAt   time.Time ` + "`" + `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"` + "`" + `
	UpdatedAt   time.Time ` + "`" + `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"` + "`" + `
}

// Create{{.Name}}Req 建立 {{.Name}} 的請求（Schema Input）
type Create{{.Name}}Req struct {
	Name        string ` + "`" + `json:"name"` + "`" + `
	Description string ` + "`" + `json:"description,omitempty"` + "`" + `
}

// Update{{.Name}}Req 更新 {{.Name}} 的請求（Schema Input）
type Update{{.Name}}Req struct {
	Name        string ` + "`" + `json:"name,omitempty"` + "`" + `
	Description string ` + "`" + `json:"description,omitempty"` + "`" + `
	Active      *bool  ` + "`" + `json:"active,omitempty"` + "`" + `
}

// {{.Name}}Resp 回應（Schema Output）
type {{.Name}}Resp struct {
	ID          int64  ` + "`" + `json:"id"` + "`" + `
	Name        string ` + "`" + `json:"name"` + "`" + `
	Description string ` + "`" + `json:"description,omitempty"` + "`" + `
	Active      bool   ` + "`" + `json:"active"` + "`" + `
	CreatedAt   string ` + "`" + `json:"created_at"` + "`" + `
	UpdatedAt   string ` + "`" + `json:"updated_at"` + "`" + `
}

// {{.Name}}ListResp 列表回應（Schema Output）
type {{.Name}}ListResp struct {
	Data  []{{.Name}}Resp ` + "`" + `json:"data"` + "`" + `
	Total int            ` + "`" + `json:"total"` + "`" + `
}
`

// serviceTemplate — 業務邏輯 + Error Catalog
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

// ============================================================
// CLI 專案模板
// ============================================================

// cliMainTemplate — CLI 專案的 main.go
const cliMainTemplate = `package main

import (
	"fmt"
	"os"

	"{{.ModuleName}}/app/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

// cliRootTemplate — CLI 專案的 app/commands/root.go
const cliRootTemplate = `package commands

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "{{.LowerName}}",
	Short: "{{.Name}} CLI tool",
	Long:  "{{.Name}} is a CLI tool built with HypGo scaffold.",
}

// Execute 執行根命令
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 全域 flags
	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
}
`

// cliCommandTemplate — CLI 子命令模板
const cliCommandTemplate = `package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var {{.LowerName}}Cmd = &cobra.Command{
	Use:   "{{.LowerName}}",
	Short: "{{.Name}} command",
	Long:  "Execute the {{.LowerName}} operation.",
	RunE:  run{{.Name}},
}

func init() {
	rootCmd.AddCommand({{.LowerName}}Cmd)

	// 命令 flags
	// {{.LowerName}}Cmd.Flags().StringP("input", "i", "", "Input file path")
	// {{.LowerName}}Cmd.Flags().StringP("output", "o", "", "Output file path")
	// {{.LowerName}}Cmd.Flags().BoolP("verbose", "v", false, "Verbose output")
}

func run{{.Name}}(cmd *cobra.Command, args []string) error {
	fmt.Println("Running {{.LowerName}} command...")

	// TODO: implement {{.LowerName}} logic

	return nil
}
`

// cliConfigTemplate — CLI 專案的 config.yaml
const cliConfigTemplate = `# {{.Name}} Configuration
app:
  name: "{{.LowerName}}"
  version: "0.1.0"

database:
  driver: ""
  dsn: ""

logger:
  level: info
  output: stdout
  colors: true
`

// cliGoModTemplate — CLI 專案的 go.mod 模板
const cliGoModTemplate = `module {{.ModuleName}}

go 1.24

require (
	github.com/maoxiaoyue/hypgo v0.8.1-alpha
	github.com/spf13/cobra v1.9.1
)
`

// ============================================================
// Desktop 專案模板（Fyne）
// ============================================================

// desktopMainTemplate — Desktop 專案的 main.go
const desktopMainTemplate = `package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"{{.ModuleName}}/app/views"
)

func main() {
	a := app.New()
	w := a.NewWindow("{{.Name}}")
	w.Resize(fyne.NewSize(800, 600))

	// 載入主畫面
	w.SetContent(views.MainView(w))

	w.ShowAndRun()
}
`

// desktopViewTemplate — Desktop 專案的 app/views/main_view.go
const desktopViewTemplate = `package views

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// MainView 回傳應用程式主畫面
func MainView(w fyne.Window) fyne.CanvasObject {
	title := widget.NewLabel("Welcome to {{.Name}}")
	title.TextStyle = fyne.TextStyle{Bold: true}

	info := widget.NewLabel("Built with HypGo + Fyne")

	return container.NewVBox(
		title,
		widget.NewSeparator(),
		info,
		widget.NewButton("Click me", func() {
			info.SetText("Button clicked!")
		}),
	)
}
`

// desktopCustomViewTemplate — 自訂 view 模板
const desktopCustomViewTemplate = `package views

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// {{.Name}}View 回傳 {{.Name}} 畫面
func {{.Name}}View(w fyne.Window) fyne.CanvasObject {
	title := widget.NewLabel("{{.Name}}")
	title.TextStyle = fyne.TextStyle{Bold: true}

	// TODO: implement {{.LowerName}} view layout

	return container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("{{.Name}} view content goes here"),
	)
}
`

// desktopConfigTemplate — Desktop 專案的 config.yaml
const desktopConfigTemplate = `# {{.Name}} Configuration
app:
  name: "{{.LowerName}}"
  version: "0.1.0"
  width: 800
  height: 600

database:
  driver: ""
  dsn: ""

logger:
  level: info
  output: stdout
  colors: true
`

// desktopGoModTemplate — Desktop 專案的 go.mod
const desktopGoModTemplate = `module {{.ModuleName}}

go 1.24

require (
	fyne.io/fyne/v2 v2.5.4
	github.com/maoxiaoyue/hypgo v0.8.1-alpha
)
`
