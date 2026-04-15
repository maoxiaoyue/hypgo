package scaffold

// controllerTemplate — 只放 handler 邏輯，路由定義移至 routers/
const controllerTemplate = `package controllers

import (
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/errors"
	"{{.ModuleName}}/app/models"
)

// @ai: madeby {{.AIProvider}}
// {{.Name}}Controller handles {{.LowerName}} CRUD operations
type {{.Name}}Controller struct{}

// 預定義錯誤碼
var (
	Err{{.Name}}NotFound = errors.Define("E_{{.LowerName}}_001", 404, "{{.Name}} not found", "{{.LowerName}}")
	Err{{.Name}}Invalid  = errors.Define("E_{{.LowerName}}_002", 400, "Invalid {{.LowerName}} data", "{{.LowerName}}")
)

// @ai: madeby {{.AIProvider}}
func (ctrl *{{.Name}}Controller) List(c *hypcontext.Context) {
	// TODO: Fetch from database
	c.JSON(200, models.{{.Name}}ListResp{
		Data:  []models.{{.Name}}Resp{},
		Total: 0,
	})
}

// @ai: madeby {{.AIProvider}}
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

// @ai: madeby {{.AIProvider}}
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

// @ai: madeby {{.AIProvider}}
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

// @ai: madeby {{.AIProvider}}
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

// @ai: madeby {{.AIProvider}}
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

// @ai: madeby {{.AIProvider}}
func New{{.Name}}Service(db *hidb.Database, logger *logger.Logger) *{{.Name}}Service {
	return &{{.Name}}Service{db: db, logger: logger}
}

// @ai: madeby {{.AIProvider}}
func (s *{{.Name}}Service) Create(ctx context.Context, data map[string]interface{}) error {
	s.logger.Info("Creating {{.LowerName}}")
	// TODO: implement
	return nil
}

// @ai: madeby {{.AIProvider}}
func (s *{{.Name}}Service) GetByID(ctx context.Context, id string) (interface{}, error) {
	s.logger.Info("Getting {{.LowerName}} by ID: %s", id)
	// TODO: implement
	return nil, ErrSvc{{.Name}}NotFound.With("id", id)
}

// @ai: madeby {{.AIProvider}}
func (s *{{.Name}}Service) Update(ctx context.Context, id string, data map[string]interface{}) error {
	s.logger.Info("Updating {{.LowerName}} ID: %s", id)
	// TODO: implement
	return nil
}

// @ai: madeby {{.AIProvider}}
func (s *{{.Name}}Service) Delete(ctx context.Context, id string) error {
	s.logger.Info("Deleting {{.LowerName}} ID: %s", id)
	// TODO: implement
	return nil
}

// @ai: madeby {{.AIProvider}}
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

// ============================================================
// gRPC 專案模板
// ============================================================

// grpcMainTemplate — gRPC 專案的 main.go
const grpcMainTemplate = `package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"{{.ModuleName}}/app/rpc"
	pb "{{.ModuleName}}/app/proto/{{.LowerName}}pb"
)

func main() {
	addr := ":9090"

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer(
		// Interceptor（中間件）可在此加入：
		// grpc.UnaryInterceptor(interceptor.Logger()),
		// grpc.ChainUnaryInterceptor(interceptor.Recovery(), interceptor.Auth()),
	)

	// 註冊服務
	pb.Register{{.Name}}ServiceServer(s, rpc.New{{.Name}}Server())

	// 啟用 gRPC reflection（方便 grpcurl 偵錯）
	reflection.Register(s)

	log.Printf("gRPC server listening on %s", addr)

	// 優雅關閉
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gRPC server...")
	s.GracefulStop()
	log.Println("Server stopped")
}
`

// grpcProtoTemplate — gRPC 專案的 .proto 檔案
const grpcProtoTemplate = `syntax = "proto3";

package {{.LowerName}}pb;

option go_package = "{{.ModuleName}}/app/proto/{{.LowerName}}pb";

// {{.Name}}Service 定義 {{.Name}} 相關的 gRPC 服務
service {{.Name}}Service {
  // Create{{.Name}} 建立新的 {{.Name}}
  rpc Create{{.Name}} (Create{{.Name}}Request) returns ({{.Name}}Response);

  // Get{{.Name}} 根據 ID 取得 {{.Name}}
  rpc Get{{.Name}} (Get{{.Name}}Request) returns ({{.Name}}Response);

  // List{{.Name}}s 列出所有 {{.Name}}
  rpc List{{.Name}}s (List{{.Name}}sRequest) returns (List{{.Name}}sResponse);

  // Update{{.Name}} 更新 {{.Name}}
  rpc Update{{.Name}} (Update{{.Name}}Request) returns ({{.Name}}Response);

  // Delete{{.Name}} 刪除 {{.Name}}
  rpc Delete{{.Name}} (Delete{{.Name}}Request) returns (Delete{{.Name}}Response);
}

// Create{{.Name}}Request 建立請求
message Create{{.Name}}Request {
  string name = 1;
  string description = 2;
}

// Get{{.Name}}Request 查詢請求
message Get{{.Name}}Request {
  int64 id = 1;
}

// List{{.Name}}sRequest 列表請求
message List{{.Name}}sRequest {
  int32 page = 1;
  int32 page_size = 2;
}

// Update{{.Name}}Request 更新請求
message Update{{.Name}}Request {
  int64 id = 1;
  string name = 2;
  string description = 3;
}

// Delete{{.Name}}Request 刪除請求
message Delete{{.Name}}Request {
  int64 id = 1;
}

// {{.Name}}Response 單筆回應
message {{.Name}}Response {
  int64 id = 1;
  string name = 2;
  string description = 3;
  string created_at = 4;
  string updated_at = 5;
}

// List{{.Name}}sResponse 列表回應
message List{{.Name}}sResponse {
  repeated {{.Name}}Response items = 1;
  int32 total = 2;
}

// Delete{{.Name}}Response 刪除回應
message Delete{{.Name}}Response {
  bool success = 1;
}
`

// grpcServerTemplate — gRPC 服務實作（app/rpc/<name>_server.go）
const grpcServerTemplate = `package rpc

import (
	"context"
	"fmt"

	pb "{{.ModuleName}}/app/proto/{{.LowerName}}pb"
)

// {{.Name}}Server 實作 {{.Name}}Service gRPC 服務
type {{.Name}}Server struct {
	pb.Unimplemented{{.Name}}ServiceServer
}

// New{{.Name}}Server 建立新的 {{.Name}} gRPC 服務
func New{{.Name}}Server() *{{.Name}}Server {
	return &{{.Name}}Server{}
}

func (s *{{.Name}}Server) Create{{.Name}}(ctx context.Context, req *pb.Create{{.Name}}Request) (*pb.{{.Name}}Response, error) {
	// TODO: implement create logic
	fmt.Printf("Create{{.Name}}: name=%s\n", req.Name)

	return &pb.{{.Name}}Response{
		Id:   1,
		Name: req.Name,
		Description: req.Description,
	}, nil
}

func (s *{{.Name}}Server) Get{{.Name}}(ctx context.Context, req *pb.Get{{.Name}}Request) (*pb.{{.Name}}Response, error) {
	// TODO: implement get logic
	fmt.Printf("Get{{.Name}}: id=%d\n", req.Id)

	return &pb.{{.Name}}Response{
		Id:   req.Id,
		Name: "example",
	}, nil
}

func (s *{{.Name}}Server) List{{.Name}}s(ctx context.Context, req *pb.List{{.Name}}sRequest) (*pb.List{{.Name}}sResponse, error) {
	// TODO: implement list logic
	return &pb.List{{.Name}}sResponse{
		Items: []*pb.{{.Name}}Response{},
		Total: 0,
	}, nil
}

func (s *{{.Name}}Server) Update{{.Name}}(ctx context.Context, req *pb.Update{{.Name}}Request) (*pb.{{.Name}}Response, error) {
	// TODO: implement update logic
	return &pb.{{.Name}}Response{
		Id:   req.Id,
		Name: req.Name,
	}, nil
}

func (s *{{.Name}}Server) Delete{{.Name}}(ctx context.Context, req *pb.Delete{{.Name}}Request) (*pb.Delete{{.Name}}Response, error) {
	// TODO: implement delete logic
	return &pb.Delete{{.Name}}Response{
		Success: true,
	}, nil
}
`

// grpcConfigTemplate — gRPC 專案的 config.yaml
const grpcConfigTemplate = `# {{.Name}} gRPC Service Configuration
app:
  name: "{{.LowerName}}"
  version: "0.1.0"

grpc:
  addr: ":9090"
  # tls:
  #   enabled: false
  #   cert_file: ""
  #   key_file: ""

database:
  driver: ""
  dsn: ""

logger:
  level: info
  output: stdout
  colors: true
`

// grpcGoModTemplate — gRPC 專案的 go.mod
const grpcGoModTemplate = `module {{.ModuleName}}

go 1.24

require (
	github.com/maoxiaoyue/hypgo v0.8.1-alpha
	google.golang.org/grpc v1.72.0
	google.golang.org/protobuf v1.36.8
)
`

// grpcMakefileTemplate — gRPC 專案的 Makefile（protoc 編譯）
const grpcMakefileTemplate = `# Generate Go code from .proto files
.PHONY: proto
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       app/proto/{{.LowerName}}pb/{{.LowerName}}.proto

# Run the server
.PHONY: run
run:
	go run main.go

# Test
.PHONY: test
test:
	go test ./...

# Build
.PHONY: build
build:
	go build -o bin/{{.LowerName}} .
`
