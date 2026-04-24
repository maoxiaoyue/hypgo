package json

import (
	"strings"
	"testing"
)

func TestMap2JSON(t *testing.T) {
	m := map[string]interface{}{"name": "chris", "age": 30}
	s, err := Map2JSON(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `"name":"chris"`) || !strings.Contains(s, `"age":30`) {
		t.Errorf("unexpected: %s", s)
	}
}

func TestMap2JSONNil(t *testing.T) {
	s, err := Map2JSON(nil)
	if err != nil || s != "null" {
		t.Errorf("got %q err=%v", s, err)
	}
}

func TestMap2JSONIndent(t *testing.T) {
	m := map[string]interface{}{"k": "v"}
	s, err := Map2JSONIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "\n") {
		t.Errorf("expected indented output, got %q", s)
	}
}

func TestJSON2Map(t *testing.T) {
	m, err := JSON2Map(`{"name":"chris","age":30}`)
	if err != nil {
		t.Fatal(err)
	}
	if m["name"] != "chris" {
		t.Errorf("name=%v", m["name"])
	}
}

func TestJSON2MapEmpty(t *testing.T) {
	if _, err := JSON2Map(""); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestJSON2MapInvalid(t *testing.T) {
	if _, err := JSON2Map("not json"); err == nil {
		t.Error("expected error for invalid json")
	}
}

func TestRoundTrip(t *testing.T) {
	orig := map[string]interface{}{"a": "1", "b": float64(2)}
	s, err := Map2JSON(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := JSON2Map(s)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"].(float64) != 2 {
		t.Errorf("roundtrip mismatch: %v", got)
	}
}

func TestBytesVariants(t *testing.T) {
	b, err := Map2JSONBytes(map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	m, err := JSON2MapBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if m["x"].(float64) != 1 {
		t.Errorf("got %v", m)
	}
}
