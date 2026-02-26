package json

import (
	"testing"
)

type TestUser struct {
	Name  string `json:"name" validate:"required"`
	Age   int    `json:"age" validate:"min=18"`
	Email string `json:"email,omitempty" validate:"omitempty,email"`
}

func TestValidator_ValidatedUnmarshal(t *testing.T) {
	v := NewValidator()

	// Valid struct
	validJSON := []byte(`{"name":"Alice", "age":25, "email":"alice@example.com"}`)
	var u TestUser
	if err := v.ValidatedUnmarshal(validJSON, &u); err != nil {
		t.Errorf("Expected valid JSON to pass validation, got error: %v", err)
	}

	// Invalid struct: missing required name
	var u2 TestUser
	invalidJSON1 := []byte(`{"age":20}`)
	if err := v.ValidatedUnmarshal(invalidJSON1, &u2); err == nil {
		t.Errorf("Expected validation error for missing name, but got nil")
	}

	// Invalid struct: age too low
	var u3 TestUser
	invalidJSON2 := []byte(`{"name":"Bob", "age":10}`)
	if err := v.ValidatedUnmarshal(invalidJSON2, &u3); err == nil {
		t.Errorf("Expected validation error for age < 18, but got nil")
	}

	// Invalid struct: bad email format
	var u4 TestUser
	invalidJSON3 := []byte(`{"name":"Charlie", "age":30, "email":"not-an-email"}`)
	if err := v.ValidatedUnmarshal(invalidJSON3, &u4); err == nil {
		t.Errorf("Expected validation error for invalid email, but got nil")
	}
}

func TestValidateWithSchema(t *testing.T) {
	schema := Schema{
		Type:     "object",
		Required: []string{"username"},
		Properties: map[string]Property{
			"username": {
				Type:      "string",
				MinLength: intPtr(3),
				MaxLength: intPtr(20),
			},
			"age": {
				Type:    "number",
				Minimum: floatPtr(0),
			},
		},
	}

	validJSON := []byte(`{"username":"hypgo_user", "age":25}`)
	if err := ValidateWithSchema(validJSON, schema); err != nil {
		t.Errorf("Expected valid JSON to pass schema validation, got error: %v", err)
	}

	invalidJSON1 := []byte(`{"age":25}`) // missing username
	if err := ValidateWithSchema(invalidJSON1, schema); err == nil {
		t.Errorf("Expected schema validation error for missing username, got nil")
	}

	invalidJSON2 := []byte(`{"username":"ab", "age":25}`) // username too short
	if err := ValidateWithSchema(invalidJSON2, schema); err == nil {
		t.Errorf("Expected schema validation error for short username, got nil")
	}

	invalidJSON3 := []byte(`{"username":"hypgo_user", "age":-5}`) // negative age
	if err := ValidateWithSchema(invalidJSON3, schema); err == nil {
		t.Errorf("Expected schema validation error for negative age, got nil")
	}
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}

func TestMarshal(t *testing.T) {
	obj := map[string]interface{}{"key": "value"}

	compact, err := MarshalCompact(obj)
	if err != nil {
		t.Fatalf("MarshalCompact failed: %v", err)
	}
	if string(compact) != `{"key":"value"}` {
		t.Errorf("MarshalCompact gave unexpected result: %s", string(compact))
	}

	indented, err := Marshal(obj)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expectedIndented := `{
  "key": "value"
}`
	if string(indented) != expectedIndented {
		t.Errorf("Marshal gave unexpected result: got %s", string(indented))
	}
}
