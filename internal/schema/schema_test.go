package schema

import (
	"strings"
	"testing"
)

// ─── Infer ────────────────────────────────────────────────────────────────────

func TestInfer_BasicTypes(t *testing.T) {
	entities := []map[string]any{
		{
			"id":     float64(1),
			"name":   "alice",
			"active": true,
		},
	}
	cols := Infer(entities)
	byName := toMap(cols)

	assertType(t, byName, "id", "DOUBLE")
	assertType(t, byName, "name", "VARCHAR(65533)")
	assertType(t, byName, "active", "BOOLEAN")
}

func TestInfer_ArrayOfStringsBecomesArrayVarchar(t *testing.T) {
	entities := []map[string]any{
		{"tags": []any{"a", "b"}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "tags", "ARRAY<VARCHAR(65533)>")
}

func TestInfer_ArrayOfNumbersBecomesArrayDouble(t *testing.T) {
	entities := []map[string]any{
		{"scores": []any{float64(1), float64(2)}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "scores", "ARRAY<DOUBLE>")
}

func TestInfer_ArrayOfBooleansBecomesArrayBoolean(t *testing.T) {
	entities := []map[string]any{
		{"flags": []any{true, false}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "flags", "ARRAY<BOOLEAN>")
}

func TestInfer_ArrayMixedScalarsWidensElementType(t *testing.T) {
	// bool and float64 in the same array → element type widens to DOUBLE
	entities := []map[string]any{
		{"v": []any{true, float64(1.5)}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "ARRAY<DOUBLE>")
}

func TestInfer_ArrayOfObjectsBecomesArrayJSON(t *testing.T) {
	entities := []map[string]any{
		{"items": []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2)}}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "items", "ARRAY<JSON>")
}

func TestInfer_EmptyArrayBecomesArrayJSON(t *testing.T) {
	entities := []map[string]any{
		{"tags": []any{}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "tags", "ARRAY<JSON>")
}

func TestInfer_ArrayWithNullElementsOnlyBecomesArrayJSON(t *testing.T) {
	entities := []map[string]any{
		{"tags": []any{nil, nil}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "tags", "ARRAY<JSON>")
}

func TestInfer_MapValueBecomesJSON(t *testing.T) {
	// map[string]any leaf — produced when flattening is disabled or max_depth is hit.
	entities := []map[string]any{
		{"meta": map[string]any{"color": "red"}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "meta", "JSON")
}

func TestInfer_NullOnlyColumnFallsBackToSTRING(t *testing.T) {
	entities := []map[string]any{
		{"x": nil},
		{"x": nil},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "x", "STRING")
}

func TestInfer_JSONWidensOverArray(t *testing.T) {
	// One row has an array, another row has a plain object for the same column.
	// JSON is wider than ARRAY<*>.
	entities := []map[string]any{
		{"v": []any{"a", "b"}},
		{"v": map[string]any{"x": 1}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "JSON")
}

func TestInfer_ArrayVsScalarMismatchFallsBackToSTRING(t *testing.T) {
	// A column that is a scalar in one row and an array in another is a schema
	// mismatch — degrade gracefully to STRING.
	entities := []map[string]any{
		{"v": float64(1)},
		{"v": []any{"a", "b"}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "STRING")
}

func TestInfer_TypeWidening_SameTypeStaysVarchar(t *testing.T) {
	// Two rows both produce VARCHAR — the column type should stay VARCHAR.
	entities := []map[string]any{
		{"v": "text"},
		{"v": "other"},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "VARCHAR(65533)")
}

func TestInfer_NullThenValueResolvesToType(t *testing.T) {
	entities := []map[string]any{
		{"x": nil},
		{"x": float64(42)},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "x", "DOUBLE")
}

func TestInfer_TypeWidening_BoolThenFloat(t *testing.T) {
	// DOUBLE is wider than BOOLEAN — should win.
	entities := []map[string]any{
		{"v": true},
		{"v": float64(1.5)},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "DOUBLE")
}

func TestInfer_TypeWidening_StringWinsOverAll(t *testing.T) {
	entities := []map[string]any{
		{"v": float64(1)},
		{"v": "text"},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "VARCHAR(65533)")
}

func TestInfer_TypeWidening_VarcharVsArrayFallsBackToSTRING(t *testing.T) {
	// A column that is a VARCHAR string in one row and an ARRAY in another is
	// a cross-category mismatch — must degrade to STRING.
	entities := []map[string]any{
		{"v": "text"},
		{"v": []any{float64(1), float64(2), float64(3)}},
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "v", "STRING")
}

func TestInfer_ColumnsAreSortedAlphabetically(t *testing.T) {
	entities := []map[string]any{
		{"z": "last", "a": "first", "m": "middle"},
	}
	cols := Infer(entities)
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	if cols[0].Name != "a" || cols[1].Name != "m" || cols[2].Name != "z" {
		t.Errorf("columns not sorted: %v", cols)
	}
}

func TestInfer_MissingColumnInSomeRowsIsStillIncluded(t *testing.T) {
	entities := []map[string]any{
		{"a": float64(1)},
		{"b": "hello"}, // "a" absent here
	}
	cols := Infer(entities)
	byName := toMap(cols)
	if _, ok := byName["a"]; !ok {
		t.Error("column 'a' missing even though it appeared in at least one row")
	}
	if _, ok := byName["b"]; !ok {
		t.Error("column 'b' missing even though it appeared in at least one row")
	}
}

func TestInfer_EmptyEntities(t *testing.T) {
	cols := Infer([]map[string]any{})
	if len(cols) != 0 {
		t.Errorf("expected empty column list, got %d columns", len(cols))
	}
}

func TestInfer_CaseCollisionMerged(t *testing.T) {
	// Simulates what happens with ERMA2 fieldvalues that arrive as "OEM" in one
	// row and "oem" in another (or mixed-case variants across releases).
	// Both must collapse to a single "oem" column — not two separate ones —
	// because Doris column names are case-insensitive.
	entities := []map[string]any{
		{"fieldvalues.OEM": "AU"},
		{"fieldvalues.oem": "VW"},
	}
	cols := Infer(entities)
	if len(cols) != 1 {
		t.Fatalf("expected 1 column after case normalisation, got %d: %v", len(cols), cols)
	}
	if cols[0].Name != "fieldvalues.oem" {
		t.Errorf("expected normalised column name %q, got %q", "fieldvalues.oem", cols[0].Name)
	}
	assertType(t, toMap(cols), "fieldvalues.oem", "VARCHAR(65533)")
}

func TestInfer_CaseNormalisationWidensType(t *testing.T) {
	// When the same logical column appears under different cases with different
	// value types, the wider type must win.
	entities := []map[string]any{
		{"Score": true},        // BOOLEAN via "Score"
		{"score": float64(99)}, // DOUBLE via "score" — wider, should win
	}
	cols := Infer(entities)
	byName := toMap(cols)
	assertType(t, byName, "score", "DOUBLE")
}

// ─── DDL ──────────────────────────────────────────────────────────────────────

func TestDDL_ContainsTableName(t *testing.T) {
	cols := []Column{{Name: "id", DorisType: "DOUBLE"}}
	ddl := DDL("mytable", cols)
	if !strings.Contains(ddl, "`mytable`") {
		t.Errorf("DDL missing table name: %s", ddl)
	}
	// Doris does not allow database-prefixed table names in CREATE TABLE;
	// the database name must not appear in the generated statement.
	if strings.Contains(ddl, "mydb") {
		t.Errorf("DDL must not contain a database name prefix: %s", ddl)
	}
}

func TestDDL_ContainsAllColumns(t *testing.T) {
	cols := []Column{
		{Name: "id", DorisType: "DOUBLE"},
		{Name: "name", DorisType: "VARCHAR(65533)"},
	}
	ddl := DDL("t", cols)
	if !strings.Contains(ddl, "`id` DOUBLE") {
		t.Errorf("DDL missing id column: %s", ddl)
	}
	if !strings.Contains(ddl, "`name` VARCHAR(65533)") {
		t.Errorf("DDL missing name column: %s", ddl)
	}
}

func TestDDL_EmptyColumns(t *testing.T) {
	ddl := DDL("t", nil)
	if !strings.Contains(ddl, "No columns") {
		t.Errorf("expected fallback message for empty columns: %s", ddl)
	}
}

func TestDDL_DuplicateKeyUsesFirstColumn(t *testing.T) {
	cols := []Column{
		{Name: "aaa", DorisType: "DOUBLE"},
		{Name: "zzz", DorisType: "VARCHAR(65533)"},
	}
	ddl := DDL("t", cols)
	if !strings.Contains(ddl, "DUPLICATE KEY(`aaa`)") {
		t.Errorf("expected DUPLICATE KEY on first (alphabetical) column: %s", ddl)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func toMap(cols []Column) map[string]string {
	m := make(map[string]string, len(cols))
	for _, c := range cols {
		m[c.Name] = c.DorisType
	}
	return m
}

func assertType(t *testing.T, byName map[string]string, col, want string) {
	t.Helper()
	got, ok := byName[col]
	if !ok {
		t.Errorf("column %q not found in inferred schema", col)
		return
	}
	if got != want {
		t.Errorf("column %q: expected type %q, got %q", col, want, got)
	}
}
