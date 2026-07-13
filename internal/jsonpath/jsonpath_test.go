package jsonpath

import "testing"

func TestGet_SimpleKey(t *testing.T) {
	data := map[string]any{"name": "alice"}
	val, err := Get(data, "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "alice" {
		t.Errorf("expected alice, got %v", val)
	}
}

func TestGet_NestedKey(t *testing.T) {
	data := map[string]any{
		"meta": map[string]any{
			"pagination": map[string]any{
				"totalPages": float64(5),
			},
		},
	}
	val, err := Get(data, "meta.pagination.totalPages")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != float64(5) {
		t.Errorf("expected 5.0, got %v", val)
	}
}

func TestGet_EmptyPath_ReturnsRoot(t *testing.T) {
	data := map[string]any{"x": 1}
	val, err := Get(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := val.(map[string]any); !ok {
		t.Errorf("expected map[string]any at root, got %T", val)
	}
}

func TestGet_MissingKey(t *testing.T) {
	data := map[string]any{"a": 1}
	_, err := Get(data, "b")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestGet_NotAnObjectIntermediate(t *testing.T) {
	data := map[string]any{"a": "string_value"}
	_, err := Get(data, "a.b")
	if err == nil {
		t.Fatal("expected error when intermediate value is not an object")
	}
}

func TestGetInt_Float64(t *testing.T) {
	data := map[string]any{"total": float64(42)}
	n, err := GetInt(data, "total")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestGetInt_IntValue(t *testing.T) {
	data := map[string]any{"total": 7}
	n, err := GetInt(data, "total")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 7 {
		t.Errorf("expected 7, got %d", n)
	}
}

func TestGetInt_StringValue(t *testing.T) {
	data := map[string]any{"total": "99"}
	n, err := GetInt(data, "total")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 99 {
		t.Errorf("expected 99, got %d", n)
	}
}

func TestGetInt_InvalidString(t *testing.T) {
	data := map[string]any{"total": "not-a-number"}
	_, err := GetInt(data, "total")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestGetInt_UnsupportedType(t *testing.T) {
	data := map[string]any{"total": []int{1, 2}}
	_, err := GetInt(data, "total")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestGetString_StringValue(t *testing.T) {
	data := map[string]any{"name": "bob"}
	s, err := GetString(data, "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "bob" {
		t.Errorf("expected bob, got %q", s)
	}
}

func TestGetString_NonStringValue(t *testing.T) {
	data := map[string]any{"count": float64(3)}
	s, err := GetString(data, "count")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == "" {
		t.Error("expected non-empty string for float64 value")
	}
}
