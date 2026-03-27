package diagnostic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

func dummyAuth(c *hypcontext.Context) {
	// pass-through auth for testing
}

func setupRouter() *router.Router {
	schema.Global().Reset()
	r := router.New()
	r.GET("/health", func(c *hypcontext.Context) {
		c.JSON(200, "ok")
	})
	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/users",
		Summary: "Create user",
	}).Handle(func(c *hypcontext.Context) {
		c.JSON(201, "created")
	})
	return r
}

// --- Register ---

func TestRegisterPanicsWithoutAuth(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("should panic without Auth")
		}
	}()
	r := router.New()
	Register(r, Config{})
}

func TestRegisterEndpoint(t *testing.T) {
	r := setupRouter()
	Register(r, Config{Auth: dummyAuth})

	req := httptest.NewRequest("GET", "/_debug/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// 驗證安全標頭
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("should set X-Content-Type-Options: nosniff")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("should set Cache-Control: no-store")
	}

	var state DiagnosticState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if state.Runtime.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", state.Runtime.GoVersion, runtime.Version())
	}
}

func TestRegisterCustomPath(t *testing.T) {
	r := router.New()
	schema.Global().Reset()
	Register(r, Config{
		Auth: dummyAuth,
		Path: "/custom/diag",
	})

	req := httptest.NewRequest("GET", "/custom/diag", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("custom path: status = %d, want 200", w.Code)
	}
}

// --- Rate Limiting ---

func TestRateLimiting(t *testing.T) {
	r := router.New()
	schema.Global().Reset()
	Register(r, Config{
		Auth:                 dummyAuth,
		MaxRequestsPerMinute: 2,
	})

	// 前 2 次應該成功
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/_debug/state", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("request %d: status = %d, want 200", i+1, w.Code)
		}
	}

	// 第 3 次應該被限流
	req := httptest.NewRequest("GET", "/_debug/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited: status = %d, want 429", w.Code)
	}
}

// --- Collector ---

func TestCollectBasic(t *testing.T) {
	r := setupRouter()
	state := Collect(r, nil, true)

	if state.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if state.Uptime == "" {
		t.Error("uptime should not be empty")
	}
	if state.Runtime.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
	if state.Runtime.NumCPU == 0 {
		t.Error("NumCPU should not be 0")
	}
}

func TestCollectRoutes(t *testing.T) {
	r := setupRouter()
	state := Collect(r, nil, true)

	if len(state.Routes) < 2 {
		t.Fatalf("routes count = %d, want >= 2", len(state.Routes))
	}

	// 驗證 schema 標記
	hasSchema := false
	for _, route := range state.Routes {
		if route.Method == "POST" && route.Path == "/api/users" {
			if !route.Schema {
				t.Error("POST /api/users should have schema=true")
			}
			hasSchema = true
		}
	}
	if !hasSchema {
		t.Error("should find POST /api/users")
	}
}

func TestCollectWithConfig(t *testing.T) {
	r := setupRouter()
	cfg := &config.Config{}
	cfg.Server.Addr = ":8080"
	cfg.Server.Protocol = "http2"
	cfg.Server.TLS.Enabled = true
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = "postgres://user:secret@localhost:5432/mydb"

	state := Collect(r, cfg, true)

	if state.Server == nil {
		t.Fatal("server should not be nil")
	}
	if state.Server.Addr != ":8080" {
		t.Errorf("addr = %q", state.Server.Addr)
	}
	if state.Database == nil {
		t.Fatal("database should not be nil")
	}
	if state.Database.Driver != "postgres" {
		t.Errorf("driver = %q", state.Database.Driver)
	}
}

// --- Redact ---

func TestRedactDSNPostgres(t *testing.T) {
	dsn := "postgres://user:secret@localhost:5432/mydb"
	got := redactDSN(dsn)
	if got != "***@localhost:5432/mydb" {
		t.Errorf("redactDSN = %q, want %q", got, "***@localhost:5432/mydb")
	}
}

func TestRedactDSNMySQL(t *testing.T) {
	dsn := "root:password@tcp(localhost:3306)/mydb"
	got := redactDSN(dsn)
	if got != "***@tcp(localhost:3306)/mydb" {
		t.Errorf("redactDSN = %q, want %q", got, "***@tcp(localhost:3306)/mydb")
	}
}

func TestRedactDSNEmpty(t *testing.T) {
	if got := redactDSN(""); got != "" {
		t.Errorf("empty DSN should return empty, got %q", got)
	}
}

func TestRedactDisabled(t *testing.T) {
	r := router.New()
	schema.Global().Reset()
	cfg := &config.Config{}
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = "postgres://user:secret@localhost:5432/mydb"

	state := Collect(r, cfg, false) // redact=false
	if state.Database.Host != cfg.Database.DSN {
		t.Error("with redact=false, should show full DSN")
	}
}

// --- RegisterWithConfig ---

func TestRegisterWithConfig(t *testing.T) {
	r := setupRouter()
	cfg := &config.Config{}
	cfg.Server.Addr = ":9090"

	RegisterWithConfig(r, cfg, Config{Auth: dummyAuth})

	req := httptest.NewRequest("GET", "/_debug/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var state DiagnosticState
	json.Unmarshal(w.Body.Bytes(), &state)

	if state.Server == nil || state.Server.Addr != ":9090" {
		t.Error("should include server config")
	}
}
