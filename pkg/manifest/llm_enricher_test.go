package manifest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

func TestBuildEnrichPrompt(t *testing.T) {
	route := RouteManifest{
		Method:       "POST",
		Path:         "/api/users",
		InputType:    "CreateUserRequest",
		OutputType:   "UserResponse",
		HandlerNames: []string{"controllers.(*UserController).Create"},
		Tags:         []string{"users"},
	}

	prompt := buildEnrichPrompt([]RouteManifest{route})

	checks := []string{
		"Protocol: rest",
		"Method: POST",
		"Path: /api/users",
		"InputType: CreateUserRequest",
		"OutputType: UserResponse",
		"Handler: controllers.(*UserController).Create",
		"Tags: users",
	}
	for _, check := range checks {
		if !containsStr(prompt, check) {
			t.Errorf("prompt should contain %q", check)
		}
	}
}

func TestBuildEnrichPromptNonREST(t *testing.T) {
	route := RouteManifest{
		Protocol: "grpc",
		Command:  "UserService/CreateUser",
	}

	prompt := buildEnrichPrompt([]RouteManifest{route})

	if !containsStr(prompt, "Protocol: grpc") {
		t.Error("prompt should contain Protocol: grpc")
	}
	if !containsStr(prompt, "Command: UserService/CreateUser") {
		t.Error("prompt should contain Command")
	}
}

// enrichOne 以單條路由呼叫 EnrichRoutes，回傳回填後的路由
func enrichOne(e LLMEnricher, route RouteManifest) RouteManifest {
	routes := []RouteManifest{route}
	e.EnrichRoutes(routes)
	return routes[0]
}

func TestBuildEnrichPromptNumbersRoutes(t *testing.T) {
	prompt := buildEnrichPrompt([]RouteManifest{{Method: "GET", Path: "/a"}, {Method: "GET", Path: "/b"}})
	if !strings.HasPrefix(prompt, enrichInstruction) {
		t.Error("prompt should start with the fixed instruction")
	}
	if !strings.Contains(prompt, "Route 1:\n  Protocol: rest\n  Method: GET\n  Path: /a\n") ||
		!strings.Contains(prompt, "Route 2:\n  Protocol: rest\n  Method: GET\n  Path: /b\n") {
		t.Errorf("routes not numbered in order:\n%s", prompt)
	}
}

func TestApplyEnrichResponse(t *testing.T) {
	routes := []RouteManifest{{Path: "/a"}, {Path: "/b"}, {Path: "/c", Summary: "Existing summary"}}

	// 帶前後文字與圍欄；id 可為字串；順序不同照 id 對回；不覆蓋已有值；id 超出範圍略過
	reply := "Here you go:\n```json\n" + `[
		{"id": "2", "summary": "B", "description": "B desc"},
		{"id": 1, "summary": "A", "description": "A desc"},
		{"id": 3, "summary": "C", "description": "C desc"},
		{"id": 9, "summary": "X"}
	]` + "\n```"
	if err := applyEnrichResponse(reply, routes); err != nil {
		t.Fatal(err)
	}
	if routes[0].Summary != "A" || routes[1].Summary != "B" || routes[1].Description != "B desc" {
		t.Errorf("results not matched by id: %+v", routes)
	}
	if routes[2].Summary != "Existing summary" || routes[2].Description != "C desc" {
		t.Errorf("should fill only blank fields: %+v", routes[2])
	}
}

func TestApplyEnrichResponseWithoutIDs(t *testing.T) {
	routes := []RouteManifest{{Path: "/a"}, {Path: "/b"}}
	if err := applyEnrichResponse(`[{"summary": "A"}, {"summary": "B"}]`, routes); err != nil {
		t.Fatal(err)
	}
	if routes[0].Summary != "A" || routes[1].Summary != "B" {
		t.Errorf("items without id should map by order: %+v", routes)
	}
}

func TestApplyEnrichResponseSingleObject(t *testing.T) {
	// 只送一條路由時，接受單一物件（description 內含方括號也不影響）
	one := []RouteManifest{{Path: "/a"}}
	if err := applyEnrichResponse(`{"summary": "A", "description": "returns [items]"}`, one); err != nil {
		t.Fatal(err)
	}
	if one[0].Summary != "A" || one[0].Description != "returns [items]" {
		t.Errorf("single object not applied: %+v", one[0])
	}

	// 多條路由時單一物件無法對應，回報錯誤且不回填
	two := []RouteManifest{{Path: "/a"}, {Path: "/b"}}
	if err := applyEnrichResponse(`{"summary": "A"}`, two); err == nil || two[0].Summary != "" {
		t.Errorf("err=%v routes=%+v, want error and no change", err, two)
	}
}

func TestApplyEnrichResponseInvalid(t *testing.T) {
	for _, reply := range []string{"not json at all", ""} {
		routes := []RouteManifest{{Method: "GET", Path: "/test"}}
		if err := applyEnrichResponse(reply, routes); err == nil {
			t.Errorf("reply %q: want error", reply)
		}
		if routes[0].Summary != "" {
			t.Errorf("reply %q: route changed: %+v", reply, routes[0])
		}
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"key": "val"}`, `{"key": "val"}`},
		{`prefix {"key": "val"} suffix`, `{"key": "val"}`},
		{`no json here`, ""},
		{`{incomplete`, ""},
		{`}backwards{`, ""},
	}
	for _, tt := range tests {
		got := extractJSON(tt.input, '{', '}')
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	if got := extractJSON("x [1, 2] y", '[', ']'); got != "[1, 2]" {
		t.Errorf("array extract = %q", got)
	}
}

func TestSanitizeResponse(t *testing.T) {
	// HTML 標籤清理
	got := sanitizeResponse("<script>alert('xss')</script>")
	if containsStr(got, "<script>") {
		t.Error("should sanitize HTML tags")
	}

	// 長度限制
	long := string(make([]byte, 1000))
	got = sanitizeResponse(long)
	if len(got) > 500 {
		t.Errorf("should limit to 500 chars, got %d", len(got))
	}
}

func TestNewLLMEnricherFromConfigNone(t *testing.T) {
	cfg := &config.LLMConfig{Mode: "none"}
	enricher, err := NewLLMEnricherFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher != nil {
		t.Error("mode=none should return nil enricher")
	}
}

func TestNewLLMEnricherFromConfigNil(t *testing.T) {
	enricher, err := NewLLMEnricherFromConfig(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher != nil {
		t.Error("nil config should return nil enricher")
	}
}

func TestNewLLMEnricherFromConfigInvalidMode(t *testing.T) {
	cfg := &config.LLMConfig{Mode: "invalid"}
	_, err := NewLLMEnricherFromConfig(cfg, nil)
	if err == nil {
		t.Error("invalid mode should return error")
	}
}

func TestOllamaEnricherIntegration(t *testing.T) {
	// 模擬 Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Response: `[{"id": 1, "summary": "Create a new user account", "description": "Creates a user with name and email"}]`,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.LLMConfig{
		Mode: "ollama",
		Ollama: config.OllamaConfig{
			URL:   server.URL,
			Model: "test-model",
		},
	}
	cfg.ApplyDefaults()

	enricher, err := NewLLMEnricherFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method:    "POST",
		Path:      "/api/users",
		InputType: "CreateUserRequest",
	}

	result := enrichOne(enricher, route)
	if result.Summary != "Create a new user account" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Create a new user account")
	}
}

func TestAPIEnricherOpenAIIntegration(t *testing.T) {
	// 模擬 OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 驗證 Authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}

		resp := openAIChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `[{"id": 1, "summary": "List all products", "description": "Returns product catalog"}]`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.LLMConfig{
		Mode: "api",
		API: config.APIConfig{
			Provider: "openai",
			BaseURL:  server.URL,
			APIKey:   "test-key",
			Model:    "gpt-4o-mini",
		},
	}
	cfg.ApplyDefaults()

	enricher, err := NewLLMEnricherFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method: "GET",
		Path:   "/api/products",
	}

	result := enrichOne(enricher, route)
	if result.Summary != "List all products" {
		t.Errorf("Summary = %q, want %q", result.Summary, "List all products")
	}
}

func TestAPIEnricherAnthropicIntegration(t *testing.T) {
	// 模擬 Anthropic server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 驗證 Anthropic headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing anthropic-version header")
		}

		resp := anthropicResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `[{"id": 1, "summary": "Delete user", "description": "Removes user account"}]`},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.LLMConfig{
		Mode: "api",
		API: config.APIConfig{
			Provider: "anthropic",
			BaseURL:  server.URL,
			APIKey:   "test-key",
			Model:    "claude-haiku-4-5",
		},
	}
	cfg.ApplyDefaults()

	enricher, err := NewLLMEnricherFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method: "DELETE",
		Path:   "/api/users/:id",
	}

	result := enrichOne(enricher, route)
	if result.Summary != "Delete user" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Delete user")
	}
}

func TestOllamaEnricherServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.LLMConfig{
		Mode: "ollama",
		Ollama: config.OllamaConfig{
			URL:   server.URL,
			Model: "test",
		},
	}
	cfg.ApplyDefaults()

	enricher, err := NewLLMEnricherFromConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	route := RouteManifest{Method: "GET", Path: "/test"}
	result := enrichOne(enricher, route)

	// 錯誤時應回傳原始 route 不修改
	if result.Method != "GET" || result.Path != "/test" {
		t.Error("server error should return original route")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
