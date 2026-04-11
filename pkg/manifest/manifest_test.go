package manifest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// 測試用 struct
type createUserReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type userResp struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func dummyHandler(c *hypcontext.Context) {}

func setupTestRouter() *router.Router {
	// 每次重置全域 registry
	schema.Global().Reset()

	r := router.New()

	// 一般路由
	r.GET("/health", dummyHandler)

	// Schema 路由
	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/users",
		Summary: "Create user",
		Tags:    []string{"users"},
		Input:   createUserReq{},
		Output:  userResp{},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "User created"},
			400: {Description: "Invalid input"},
		},
	}).Handle(dummyHandler)

	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/users/:id",
		Summary: "Get user by ID",
		Tags:    []string{"users"},
		Output:  userResp{},
	}).Handle(dummyHandler)

	return r
}

// --- Collector ---

func TestCollectorCollect(t *testing.T) {
	r := setupTestRouter()
	cfg := &config.Config{}
	cfg.Server.Addr = ":8080"
	cfg.Server.Protocol = "http2"
	cfg.Server.TLS.Enabled = true

	c := NewCollector(r, cfg)
	m := c.Collect()

	if m.Framework != "HypGo" {
		t.Errorf("Framework = %q, want %q", m.Framework, "HypGo")
	}
	if m.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want %q", m.Server.Addr, ":8080")
	}
	if !m.Server.TLS {
		t.Error("Server.TLS should be true")
	}
}

func TestCollectorRoutes(t *testing.T) {
	r := setupTestRouter()
	c := NewCollector(r, nil)
	m := c.Collect()

	if len(m.Routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(m.Routes))
	}

	// 路由已按 path 排序
	// /api/users, /api/users/:id, /health
	found := false
	for _, route := range m.Routes {
		if route.Method == "POST" && route.Path == "/api/users" {
			found = true
			if route.Summary != "Create user" {
				t.Errorf("Summary = %q, want %q", route.Summary, "Create user")
			}
			if route.InputType != "createUserReq" {
				t.Errorf("InputType = %q, want %q", route.InputType, "createUserReq")
			}
			if route.OutputType != "userResp" {
				t.Errorf("OutputType = %q, want %q", route.OutputType, "userResp")
			}
			if len(route.Tags) != 1 || route.Tags[0] != "users" {
				t.Errorf("Tags = %v, want [users]", route.Tags)
			}
			if route.Responses[201] != "User created" {
				t.Errorf("Responses[201] = %q, want %q", route.Responses[201], "User created")
			}
		}
	}
	if !found {
		t.Error("POST /api/users not found in routes")
	}
}

func TestCollectorNonSchemaRoute(t *testing.T) {
	r := setupTestRouter()
	c := NewCollector(r, nil)
	m := c.Collect()

	for _, route := range m.Routes {
		if route.Path == "/health" {
			// enricher 會從 handler 名自動推斷 summary，所以不再是空的
			// 但不應有 schema 特有的欄位（InputType、OutputType）
			if route.InputType != "" {
				t.Errorf("non-schema route should have empty input_type, got %q", route.InputType)
			}
			if len(route.HandlerNames) == 0 {
				t.Error("HandlerNames should not be empty")
			}
			return
		}
	}
	t.Error("/health not found")
}

func TestCollectorDatabase(t *testing.T) {
	r := router.New()
	schema.Global().Reset()

	cfg := &config.Config{}
	cfg.Database.Driver = "postgres"

	c := NewCollector(r, cfg)
	m := c.Collect()

	if m.Database == nil {
		t.Fatal("Database should not be nil")
	}
	if m.Database.Driver != "postgres" {
		t.Errorf("Driver = %q, want %q", m.Database.Driver, "postgres")
	}
}

func TestCollectorNilConfig(t *testing.T) {
	r := router.New()
	schema.Global().Reset()

	c := NewCollector(r, nil)
	m := c.Collect()

	if m.Server.Addr != "" {
		t.Error("nil config should produce empty server info")
	}
	if m.Database != nil {
		t.Error("nil config should produce nil database")
	}
}

// --- Writer ---

func TestWriteYAML(t *testing.T) {
	r := setupTestRouter()
	c := NewCollector(r, nil)
	m := c.Collect()

	var buf bytes.Buffer
	if err := WriteYAML(&buf, m); err != nil {
		t.Fatalf("WriteYAML failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "framework: HypGo") {
		t.Error("YAML should contain framework: HypGo")
	}
	if !strings.Contains(output, "/api/users") {
		t.Error("YAML should contain route path")
	}
	if !strings.Contains(output, "Create user") {
		t.Error("YAML should contain summary")
	}
}

func TestWriteJSON(t *testing.T) {
	r := setupTestRouter()
	c := NewCollector(r, nil)
	m := c.Collect()

	var buf bytes.Buffer
	if err := WriteJSON(&buf, m); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var parsed Manifest
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON output is not valid: %v", err)
	}
	if parsed.Framework != "HypGo" {
		t.Errorf("Framework = %q, want %q", parsed.Framework, "HypGo")
	}
}

// --- Group Schema Integration ---

func TestGroupSchemaRoutes(t *testing.T) {
	schema.Global().Reset()

	r := router.New()
	api := r.NewGroup("/api/v1")

	api.Schema(schema.Route{
		Method:  "GET",
		Path:    "/products",
		Summary: "List products",
		Tags:    []string{"products"},
	}).Handle(dummyHandler)

	c := NewCollector(r, nil)
	m := c.Collect()

	found := false
	for _, route := range m.Routes {
		if route.Path == "/api/v1/products" {
			found = true
			if route.Summary != "List products" {
				t.Errorf("Summary = %q, want %q", route.Summary, "List products")
			}
		}
	}
	if !found {
		t.Error("GET /api/v1/products not found")
	}
}
