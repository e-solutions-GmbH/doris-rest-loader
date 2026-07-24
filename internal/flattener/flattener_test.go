package flattener

import (
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func enabledCfg() config.FlatteningConfig {
	t := true
	return config.FlatteningConfig{Enabled: &t, Separator: "."}
}

func disabledCfg() config.FlatteningConfig {
	f := false
	return config.FlatteningConfig{Enabled: &f, Separator: "."}
}

func TestFlatten_FlatEntity_Unchanged(t *testing.T) {
	f := New(enabledCfg())
	entity := map[string]any{"id": float64(1), "name": "alice"}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["id"] != float64(1) {
		t.Errorf("id: expected 1, got %v", result["id"])
	}
	if result["name"] != "alice" {
		t.Errorf("name: expected alice, got %v", result["name"])
	}
}

func TestFlatten_NestedObject_Flattened(t *testing.T) {
	f := New(enabledCfg())
	entity := map[string]any{
		"user": map[string]any{
			"id":   float64(42),
			"name": "bob",
		},
		"score": float64(99),
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["user.id"] != float64(42) {
		t.Errorf("user.id: expected 42, got %v", result["user.id"])
	}
	if result["user.name"] != "bob" {
		t.Errorf("user.name: expected bob, got %v", result["user.name"])
	}
	if result["score"] != float64(99) {
		t.Errorf("score: expected 99, got %v", result["score"])
	}
	if _, ok := result["user"]; ok {
		t.Error("nested 'user' key should not appear in flattened output")
	}
}

func TestFlatten_DeepNesting(t *testing.T) {
	f := New(enabledCfg())
	entity := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "deep",
			},
		},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["a.b.c"] != "deep" {
		t.Errorf("a.b.c: expected 'deep', got %v", result["a.b.c"])
	}
}

func TestFlatten_ArrayValuePreserved(t *testing.T) {
	f := New(enabledCfg())
	arr := []any{1, 2, 3}
	entity := map[string]any{"tags": arr}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["tags"]; !ok {
		t.Error("expected 'tags' key in flattened output (arrays are leaf values)")
	}
}

func TestFlatten_DisabledReturnsAsIs(t *testing.T) {
	f := New(disabledCfg())
	entity := map[string]any{
		"nested": map[string]any{"x": 1},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["nested"]; !ok {
		t.Error("expected 'nested' key when flattening is disabled")
	}
	if _, ok := result["nested.x"]; ok {
		t.Error("should not have flat key 'nested.x' when flattening is disabled")
	}
}

func TestFlatten_CustomSeparator(t *testing.T) {
	cfg := config.FlatteningConfig{Enabled: boolPtr(true), Separator: "_"}
	f := New(cfg)
	entity := map[string]any{
		"user": map[string]any{"id": float64(1)},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["user_id"] != float64(1) {
		t.Errorf("expected key 'user_id', got keys: %v", keyList(result))
	}
}

func TestFlatten_IncludeSelectsFields(t *testing.T) {
	cfg := enabledCfg()
	cfg.Include = []config.FieldMapping{
		{Path: "user.id", As: "user_id"},
		{Path: "score"},
	}
	f := New(cfg)

	entity := map[string]any{
		"user":   map[string]any{"id": float64(7), "name": "carol"},
		"score":  float64(88),
		"hidden": "should not appear",
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["user_id"] != float64(7) {
		t.Errorf("user_id: expected 7, got %v", result["user_id"])
	}
	if result["score"] != float64(88) {
		t.Errorf("score: expected 88, got %v", result["score"])
	}
	if _, ok := result["hidden"]; ok {
		t.Error("field 'hidden' should not appear in selected-field output")
	}
	if _, ok := result["user.name"]; ok {
		t.Error("non-selected nested field 'user.name' should not appear")
	}
}

func TestFlatten_IncludeAbsentFieldSkipped(t *testing.T) {
	cfg := enabledCfg()
	cfg.Include = []config.FieldMapping{
		{Path: "exists"},
		{Path: "does_not_exist"},
	}
	f := New(cfg)
	entity := map[string]any{"exists": "yes"}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["exists"] != "yes" {
		t.Errorf("expected exists=yes, got %v", result["exists"])
	}
	if _, ok := result["does_not_exist"]; ok {
		t.Error("absent field should be silently skipped")
	}
}

func TestFlatten_IncludeNestedObjectFlattened(t *testing.T) {
	cfg := enabledCfg()
	cfg.Include = []config.FieldMapping{
		{Path: "address", As: "addr"},
	}
	f := New(cfg)
	entity := map[string]any{
		"address": map[string]any{
			"city":    "Berlin",
			"country": "DE",
		},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["addr.city"] != "Berlin" {
		t.Errorf("addr.city: expected Berlin, got %v", result["addr.city"])
	}
	if result["addr.country"] != "DE" {
		t.Errorf("addr.country: expected DE, got %v", result["addr.country"])
	}
}

func TestFlattenAll_AppliestoAllEntities(t *testing.T) {
	f := New(enabledCfg())
	entities := []map[string]any{
		{"a": map[string]any{"x": float64(1)}},
		{"a": map[string]any{"x": float64(2)}},
	}
	results, err := f.FlattenAll(entities)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0]["a.x"] != float64(1) || results[1]["a.x"] != float64(2) {
		t.Errorf("flattened values mismatch: %v, %v", results[0], results[1])
	}
}

func TestFlatten_MaxDepth1_NestedObjectBecomesLeaf(t *testing.T) {
	cfg := config.FlatteningConfig{Enabled: boolPtr(true), Separator: ".", MaxDepth: 1}
	f := New(cfg)
	entity := map[string]any{
		"project": map[string]any{
			"id":   float64(1),
			"meta": map[string]any{"color": "red"},
		},
		"score": float64(99),
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Top-level scalars are passed through.
	if result["score"] != float64(99) {
		t.Errorf("score: expected 99, got %v", result["score"])
	}
	// At depth 1, "project" is a map — it must be kept as a leaf, not recursed.
	if _, ok := result["project"]; !ok {
		t.Error("expected 'project' to be a leaf map at max_depth=1")
	}
	if _, ok := result["project.id"]; ok {
		t.Error("project.id should not be flattened at max_depth=1")
	}
}

func TestFlatten_MaxDepth2_FlattensOneLevel(t *testing.T) {
	cfg := config.FlatteningConfig{Enabled: boolPtr(true), Separator: ".", MaxDepth: 2}
	f := New(cfg)
	entity := map[string]any{
		"project": map[string]any{
			"id":   float64(7),
			"meta": map[string]any{"color": "red"},
		},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Depth 1 (project.*) is unrolled.
	if result["project.id"] != float64(7) {
		t.Errorf("project.id: expected 7, got %v", result["project.id"])
	}
	// Depth 2 (project.meta.*) must be kept as a leaf map.
	if _, ok := result["project.meta"]; !ok {
		t.Error("project.meta should be a leaf map at max_depth=2")
	}
	if _, ok := result["project.meta.color"]; ok {
		t.Error("project.meta.color should not be flattened at max_depth=2")
	}
}

func TestFlatten_MaxDepth0_UnlimitedDepth(t *testing.T) {
	// MaxDepth=0 must behave exactly like no limit.
	cfg := config.FlatteningConfig{Enabled: boolPtr(true), Separator: ".", MaxDepth: 0}
	f := New(cfg)
	entity := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "deep",
			},
		},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["a.b.c"] != "deep" {
		t.Errorf("a.b.c: expected 'deep', got %v", result["a.b.c"])
	}
}

func TestFlatten_RawJSONCol_IsMapNotString(t *testing.T) {
	cfg := enabledCfg()
	cfg.RawJSON = "raw_json"
	f := New(cfg)

	entity := map[string]any{
		"key":    "FEAT-1",
		"status": map[string]any{"name": "Open"},
	}
	result, err := f.Flatten(entity)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	// raw_json column must be present.
	raw, ok := result["raw_json"]
	if !ok {
		t.Errorf("expected 'raw_json' key in flattened output; got keys: %v", keyList(result))
		return
	}

	// Value must be map[string]any, not a string (ensures Doris JSON type, not VARCHAR).
	rawMap, ok := raw.(map[string]any)
	if !ok {
		t.Errorf("raw_json value must be map[string]any, got %T; VARCHAR would mean wrong Doris column type", raw)
		return
	}
	if rawMap["key"] != "FEAT-1" {
		t.Errorf("raw_json[key]: expected FEAT-1, got %v", rawMap["key"])
	}

	// Flattened fields must also be present alongside raw_json.
	if result["key"] != "FEAT-1" {
		t.Errorf("flattened key: expected FEAT-1, got %v", result["key"])
	}
	if result["status.name"] != "Open" {
		t.Errorf("flattened status.name: expected Open, got %v", result["status.name"])
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func boolPtr(b bool) *bool { return &b }

func keyList(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
