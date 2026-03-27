package contract

import (
	"encoding/json"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// 測試用 struct
type createReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type userResp struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type partialResp struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// 建立測試用 handler
func jsonHandler(status int, body interface{}) hypcontext.HandlerFunc {
	return func(c *hypcontext.Context) {
		c.JSON(status, body)
	}
}

func setupTestRouter() *router.Router {
	schema.Global().Reset()

	r := router.New()

	// Schema 路由：POST /api/users
	r.Schema(schema.Route{
		Method:  "POST",
		Path:    "/api/users",
		Summary: "Create user",
		Input:   createReq{},
		Output:  userResp{},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "User created"},
		},
	}).Handle(jsonHandler(201, userResp{ID: 1, Name: "test", Email: "test@test.com"}))

	// Schema 路由：GET /api/users/:id
	r.Schema(schema.Route{
		Method:  "GET",
		Path:    "/api/users/:id",
		Summary: "Get user",
		Output:  userResp{},
	}).Handle(jsonHandler(200, userResp{ID: 1, Name: "test", Email: "test@test.com"}))

	// 非 schema 路由
	r.GET("/health", jsonHandler(200, map[string]string{"status": "ok"}))

	return r
}

// --- Test ---

func TestTestBasic(t *testing.T) {
	r := setupTestRouter()

	Test(t, r, TestCase{
		Route:        "GET /health",
		ExpectStatus: 200,
	})
}

func TestTestWithSchema(t *testing.T) {
	r := setupTestRouter()

	Test(t, r, TestCase{
		Route:        "POST /api/users",
		Input:        `{"name":"alice","email":"alice@test.com"}`,
		ExpectStatus: 201,
		ExpectSchema: true,
	})
}

func TestTestWithSchemaInputValidation(t *testing.T) {
	r := setupTestRouter()

	// 有效的 input — 應該通過
	Test(t, r, TestCase{
		Route:        "POST /api/users",
		Input:        `{"name":"alice","email":"alice@test.com"}`,
		ExpectStatus: 201,
		ExpectSchema: true,
	})
}

func TestTestWithBody(t *testing.T) {
	r := setupTestRouter()

	resp := userResp{ID: 1, Name: "test", Email: "test@test.com"}
	expected, _ := json.Marshal(resp)

	Test(t, r, TestCase{
		Route:      "GET /api/users/1",
		ExpectBody: string(expected),
	})
}

func TestTestWithQuery(t *testing.T) {
	r := router.New()
	schema.Global().Reset()

	r.GET("/search", func(c *hypcontext.Context) {
		q := c.Query("q")
		c.JSON(200, map[string]string{"query": q})
	})

	Test(t, r, TestCase{
		Route:        "GET /search",
		Query:        map[string]string{"q": "hello"},
		ExpectStatus: 200,
	})
}

func TestTestWithHeaders(t *testing.T) {
	r := setupTestRouter()

	Test(t, r, TestCase{
		Route:        "GET /health",
		Headers:      map[string]string{"X-Custom": "value"},
		ExpectStatus: 200,
	})
}

func TestTest404(t *testing.T) {
	r := setupTestRouter()

	Test(t, r, TestCase{
		Route:        "GET /nonexistent",
		ExpectStatus: 404,
	})
}

// --- TestAll ---

func TestTestAllRoutes(t *testing.T) {
	r := setupTestRouter()

	// TestAll 會自動測試所有 schema-registered 路由
	// 這裡有 2 個 schema 路由：POST /api/users 和 GET /api/users/:id
	TestAll(t, r)
}

func TestTestAllNoSchemas(t *testing.T) {
	schema.Global().Reset()
	r := router.New()
	r.GET("/health", jsonHandler(200, nil))

	// 無 schema 路由時應 skip
	TestAll(t, r)
}

// --- TestRoute ---

func TestTestRoute(t *testing.T) {
	r := setupTestRouter()
	TestRoute(t, r, "GET", "/health", 200)
}

// --- Validation ---

func TestValidateResponseValid(t *testing.T) {
	body := `{"id":1,"name":"test","email":"test@test.com"}`
	if err := validateResponse([]byte(body), userResp{}); err != nil {
		t.Errorf("valid response should pass: %v", err)
	}
}

func TestValidateResponseMissingField(t *testing.T) {
	body := `{"id":1}`
	err := validateResponse([]byte(body), userResp{})
	if err == nil {
		t.Error("missing required fields should fail")
	}
}

func TestValidateResponseEmpty(t *testing.T) {
	err := validateResponse([]byte{}, userResp{})
	if err == nil {
		t.Error("empty body should fail")
	}
}

func TestValidateResponseNilType(t *testing.T) {
	if err := validateResponse([]byte(`{}`), nil); err != nil {
		t.Errorf("nil type should pass: %v", err)
	}
}

func TestValidateResponseOptionalFields(t *testing.T) {
	body := `{"id":1}`
	if err := validateResponse([]byte(body), partialResp{}); err != nil {
		t.Errorf("optional fields missing should pass: %v", err)
	}
}

func TestValidateRequestValid(t *testing.T) {
	body := `{"name":"test","email":"test@test.com"}`
	if err := validateRequest([]byte(body), createReq{}); err != nil {
		t.Errorf("valid request should pass: %v", err)
	}
}

func TestValidateRequestMissing(t *testing.T) {
	body := `{"name":"test"}`
	err := validateRequest([]byte(body), createReq{})
	if err == nil {
		t.Error("missing required field should fail")
	}
}

// --- Generate ---

func TestGenerateTestCase(t *testing.T) {
	route := schema.Route{
		Method: "POST",
		Path:   "/api/users",
		Input:  createReq{},
		Output: userResp{},
		Responses: map[int]schema.ResponseSchema{
			201: {Description: "Created"},
		},
	}

	tc := generateTestCase(route)

	if tc.Route != "POST /api/users" {
		t.Errorf("Route = %q, want %q", tc.Route, "POST /api/users")
	}
	if tc.Input == "" {
		t.Error("Input should not be empty for POST")
	}
	if tc.ExpectStatus != 201 {
		t.Errorf("ExpectStatus = %d, want 201", tc.ExpectStatus)
	}
	if !tc.ExpectSchema {
		t.Error("ExpectSchema should be true when Output is set")
	}
}

func TestGenerateTestCaseWithParams(t *testing.T) {
	route := schema.Route{
		Method: "GET",
		Path:   "/api/users/:id",
		Output: userResp{},
	}

	tc := generateTestCase(route)
	if tc.Route != "GET /api/users/1" {
		t.Errorf("Route = %q, want %q (should resolve :id to 1)", tc.Route, "GET /api/users/1")
	}
}

func TestGenerateMinimalJSON(t *testing.T) {
	result := generateMinimalJSON(createReq{})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["name"]; !ok {
		t.Error("should contain 'name' field")
	}
	if _, ok := parsed["email"]; !ok {
		t.Error("should contain 'email' field")
	}
}

func TestGenerateMinimalJSONNil(t *testing.T) {
	if result := generateMinimalJSON(nil); result != "{}" {
		t.Errorf("nil type should produce {}, got %s", result)
	}
}

// --- parseRoute ---

func TestParseRoute(t *testing.T) {
	tests := []struct {
		input      string
		wantMethod string
		wantPath   string
	}{
		{"GET /api/users", "GET", "/api/users"},
		{"POST /api/users", "POST", "/api/users"},
		{"DELETE /api/users/1", "DELETE", "/api/users/1"},
		{"invalid", "", ""},
	}

	for _, tt := range tests {
		method, path := parseRoute(tt.input)
		if method != tt.wantMethod || path != tt.wantPath {
			t.Errorf("parseRoute(%q) = (%q, %q), want (%q, %q)",
				tt.input, method, path, tt.wantMethod, tt.wantPath)
		}
	}
}

// --- resolvePath ---

func TestResolvePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/api/users", "/api/users"},
		{"/api/users/:id", "/api/users/1"},
		{"/api/users/:id/posts/:postId", "/api/users/1/posts/1"},
		{"/files/*filepath", "/files/test.txt"},
		{"/api/:name", "/api/test"},
		{"/api/:slug", "/api/test-slug"},
	}

	for _, tt := range tests {
		got := resolvePath(tt.input)
		if got != tt.want {
			t.Errorf("resolvePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- guessExpectedStatus ---

func TestGuessExpectedStatus(t *testing.T) {
	tests := []struct {
		route schema.Route
		want  int
	}{
		{schema.Route{Method: "GET"}, 200},
		{schema.Route{Method: "POST"}, 201},
		{schema.Route{Method: "DELETE"}, 204},
		{schema.Route{Method: "PUT"}, 200},
		{schema.Route{Method: "POST", Responses: map[int]schema.ResponseSchema{
			200: {Description: "OK"},
		}}, 200}, // 有明確宣告時優先
	}

	for _, tt := range tests {
		got := guessExpectedStatus(tt.route)
		if got != tt.want {
			t.Errorf("guessExpectedStatus(%s) = %d, want %d", tt.route.Method, got, tt.want)
		}
	}
}
