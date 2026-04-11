package manifest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	prompt := buildEnrichPrompt(route)

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

	prompt := buildEnrichPrompt(route)

	if !containsStr(prompt, "Protocol: grpc") {
		t.Error("prompt should contain Protocol: grpc")
	}
	if !containsStr(prompt, "Command: UserService/CreateUser") {
		t.Error("prompt should contain Command")
	}
}

func TestParseEnrichResponse(t *testing.T) {
	route := RouteManifest{
		Method: "GET",
		Path:   "/api/users",
	}

	// 正常 JSON
	response := `{"summary": "List all users", "description": "Returns paginated list of users"}`
	result := parseEnrichResponse(response, route)

	if result.Summary != "List all users" {
		t.Errorf("Summary = %q, want %q", result.Summary, "List all users")
	}
	if result.Description != "Returns paginated list of users" {
		t.Errorf("Description = %q, want %q", result.Description, "Returns paginated list of users")
	}
}

func TestParseEnrichResponseWithSurroundingText(t *testing.T) {
	route := RouteManifest{}

	// LLM 回覆常帶前後文字
	response := `Here is the result: {"summary": "Create user", "description": "Creates a new user"} Hope this helps!`
	result := parseEnrichResponse(response, route)

	if result.Summary != "Create user" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Create user")
	}
}

func TestParseEnrichResponseNoOverwrite(t *testing.T) {
	route := RouteManifest{
		Summary:     "Existing summary",
		Description: "Existing description",
	}

	response := `{"summary": "New summary", "description": "New description"}`
	result := parseEnrichResponse(response, route)

	// 不覆蓋已有值
	if result.Summary != "Existing summary" {
		t.Errorf("should not overwrite existing Summary, got %q", result.Summary)
	}
	if result.Description != "Existing description" {
		t.Errorf("should not overwrite existing Description, got %q", result.Description)
	}
}

func TestParseEnrichResponseInvalid(t *testing.T) {
	route := RouteManifest{Method: "GET", Path: "/test"}

	// 無效 JSON
	result := parseEnrichResponse("not json at all", route)
	if result.Method != "GET" || result.Path != "/test" {
		t.Error("invalid response should return original route unchanged")
	}

	// 空回覆
	result = parseEnrichResponse("", route)
	if result.Method != "GET" {
		t.Error("empty response should return original route unchanged")
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
		got := extractJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
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
	enricher, err := NewLLMEnricherFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher != nil {
		t.Error("mode=none should return nil enricher")
	}
}

func TestNewLLMEnricherFromConfigNil(t *testing.T) {
	enricher, err := NewLLMEnricherFromConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher != nil {
		t.Error("nil config should return nil enricher")
	}
}

func TestNewLLMEnricherFromConfigInvalidMode(t *testing.T) {
	cfg := &config.LLMConfig{Mode: "invalid"}
	_, err := NewLLMEnricherFromConfig(cfg)
	if err == nil {
		t.Error("invalid mode should return error")
	}
}

func TestOllamaEnricherIntegration(t *testing.T) {
	// 模擬 Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Response: `{"summary": "Create a new user account", "description": "Creates a user with name and email"}`,
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

	enricher, err := NewLLMEnricherFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method:    "POST",
		Path:      "/api/users",
		InputType: "CreateUserRequest",
	}

	result := enricher.EnrichRoute(route)
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
				}{Content: `{"summary": "List all products", "description": "Returns product catalog"}`}},
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

	enricher, err := NewLLMEnricherFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method: "GET",
		Path:   "/api/products",
	}

	result := enricher.EnrichRoute(route)
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
				{Text: `{"summary": "Delete user", "description": "Removes user account"}`},
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

	enricher, err := NewLLMEnricherFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route := RouteManifest{
		Method: "DELETE",
		Path:   "/api/users/:id",
	}

	result := enricher.EnrichRoute(route)
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

	enricher, err := NewLLMEnricherFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	route := RouteManifest{Method: "GET", Path: "/test"}
	result := enricher.EnrichRoute(route)

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
