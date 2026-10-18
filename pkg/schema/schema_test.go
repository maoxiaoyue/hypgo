package schema

import (
	"reflect"
	"testing"
)

// 測試用 struct
type testUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

type testOptionalUser struct {
	Name  string  `json:"name"`
	Email string  `json:"email,omitempty"`
	Bio   *string `json:"bio"`
}

// --- TypeName ---

func TestTypeName(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{testUser{}, "testUser"},
		{&testUser{}, "testUser"},
		{(*testUser)(nil), "testUser"},
		{nil, ""},
		{42, "int"},
		{"hello", "string"},
	}

	for _, tt := range tests {
		got := TypeName(tt.input)
		if got != tt.want {
			t.Errorf("TypeName(%T) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- FieldsOf ---

func TestFieldsOf(t *testing.T) {
	fields := FieldsOf(testUser{})
	if len(fields) != 3 {
		t.Fatalf("FieldsOf(testUser{}) returned %d fields, want 3", len(fields))
	}

	// name 欄位
	if fields[0].Name != "name" || fields[0].Type != "string" || !fields[0].Required {
		t.Errorf("fields[0] = %+v, want name/string/required", fields[0])
	}

	// age 欄位
	if fields[2].Name != "age" || fields[2].Type != "integer" {
		t.Errorf("fields[2] = %+v, want age/integer", fields[2])
	}
}

func TestFieldsOfOptional(t *testing.T) {
	fields := FieldsOf(testOptionalUser{})
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(fields))
	}

	// name: required (non-pointer, no omitempty)
	if !fields[0].Required {
		t.Error("name should be required")
	}
	// email: not required (omitempty)
	if fields[1].Required {
		t.Error("email should not be required (omitempty)")
	}
	// bio: not required (pointer)
	if fields[2].Required {
		t.Error("bio should not be required (pointer)")
	}
}

func TestFieldsOfNonStruct(t *testing.T) {
	if fields := FieldsOf(42); fields != nil {
		t.Error("FieldsOf(int) should return nil")
	}
	if fields := FieldsOf(nil); fields != nil {
		t.Error("FieldsOf(nil) should return nil")
	}
}

// --- Registry ---

func TestRegistryRegisterAndGet(t *testing.T) {
	r := &Registry{}

	route := Route{
		Method:  "POST",
		Path:    "/api/users",
		Summary: "Create user",
		Tags:    []string{"users"},
		Input:   testUser{},
	}

	r.Register(route)

	if r.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", r.Len())
	}

	got, ok := r.Get("POST", "/api/users")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Summary != "Create user" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Create user")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := &Registry{}

	_, ok := r.Get("GET", "/nonexistent")
	if ok {
		t.Error("Get should return false for nonexistent route")
	}
}

func TestRegistryAll(t *testing.T) {
	r := &Registry{}

	r.Register(Route{Method: "GET", Path: "/a"})
	r.Register(Route{Method: "POST", Path: "/b"})

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d, want 2", len(all))
	}
}

func TestRegistryUpdateExisting(t *testing.T) {
	r := &Registry{}

	r.Register(Route{Method: "GET", Path: "/api/v1", Summary: "v1"})
	r.Register(Route{Method: "GET", Path: "/api/v1", Summary: "v1-updated"})

	if r.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (should update, not duplicate)", r.Len())
	}

	got, _ := r.Get("GET", "/api/v1")
	if got.Summary != "v1-updated" {
		t.Errorf("Summary = %q, want %q", got.Summary, "v1-updated")
	}
}

func TestRegistryReset(t *testing.T) {
	r := &Registry{}

	r.Register(Route{Method: "GET", Path: "/a"})
	r.Reset()

	if r.Len() != 0 {
		t.Error("Reset() should clear all schemas")
	}
}

// --- SchemaRoute Builder ---

func TestSchemaRouteAutoTypeName(t *testing.T) {
	route := Route{
		Method: "POST",
		Path:   "/test",
		Input:  testUser{},
		Output: testOptionalUser{},
	}

	// 模擬 NewSchemaRoute 的自動填入
	sr := NewSchemaRoute(route, nil)

	if sr.route.InputName != "testUser" {
		t.Errorf("InputName = %q, want %q", sr.route.InputName, "testUser")
	}
	if sr.route.OutputName != "testOptionalUser" {
		t.Errorf("OutputName = %q, want %q", sr.route.OutputName, "testOptionalUser")
	}
}

// --- goTypeToSchemaType ---

func TestGoTypeToSchemaType(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"hello", "string"},
		{42, "integer"},
		{3.14, "number"},
		{true, "boolean"},
		{[]int{}, "array"},
		{map[string]int{}, "object"},
		{testUser{}, "object"},
	}

	for _, tt := range tests {
		got := goTypeToSchemaType(reflect.TypeOf(tt.input))
		if got != tt.want {
			t.Errorf("goTypeToSchemaType(%T) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
