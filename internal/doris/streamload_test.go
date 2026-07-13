package doris

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func testLoader(serverURL string) *StreamLoader {
	return NewStreamLoader(config.DorisConfig{
		Host:     serverURL,
		Database: "testdb",
		Table:    "testtable",
		User:     "root",
		Password: "",
	})
}

func TestLoadPage_EmptyEntities_Noop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()

	loader := testLoader(srv.URL)
	if err := loader.LoadPage(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error for empty entities: %v", err)
	}
	if called {
		t.Error("expected no HTTP call for empty entity slice")
	}
}

func TestLoadPage_Success(t *testing.T) {
	var receivedBody []byte
	var receivedMethod, receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"Status": "Success", "NumberLoadedRows": 2})
	}))
	defer srv.Close()

	loader := testLoader(srv.URL)
	entities := []map[string]any{
		{"id": float64(1), "name": "alice"},
		{"id": float64(2), "name": "bob"},
	}

	if err := loader.LoadPage(context.Background(), entities); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedMethod != http.MethodPut {
		t.Errorf("expected PUT request, got %s", receivedMethod)
	}
	if receivedPath != "/api/testdb/testtable/_stream_load" {
		t.Errorf("unexpected path: %s", receivedPath)
	}

	// Verify NDJSON: two lines, each a valid JSON object.
	lines := strings.Split(strings.TrimSpace(string(receivedBody)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 NDJSON lines, got %d: %q", len(lines), string(receivedBody))
	}
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// ─── buildJSONMappings ────────────────────────────────────────────────────────

func TestBuildJSONMappings_SimpleKeys(t *testing.T) {
	entities := []map[string]any{{"id": 1, "name": "alice"}}
	paths, cols := buildJSONMappings(entities)

	if !strings.Contains(paths, "$['id']") {
		t.Errorf("expected $['id'] in jsonpaths, got: %s", paths)
	}
	if !strings.Contains(paths, "$['name']") {
		t.Errorf("expected $['name'] in jsonpaths, got: %s", paths)
	}
	if !strings.Contains(cols, "`id`") {
		t.Errorf("expected `id` in columns, got: %s", cols)
	}
	if !strings.Contains(cols, "`name`") {
		t.Errorf("expected `name` in columns, got: %s", cols)
	}
}

func TestBuildJSONMappings_DotNotationKeys(t *testing.T) {
	// This is the core ERMA2 case: flattened keys like "project.id" must use
	// bracket notation so Doris treats the dot as part of the literal key name,
	// not as a JSON path separator.
	entities := []map[string]any{{"project.id": 15, "project.name": "MyProject"}}
	paths, cols := buildJSONMappings(entities)

	if !strings.Contains(paths, "$['project.id']") {
		t.Errorf("expected bracket notation $['project.id'] in jsonpaths, got: %s", paths)
	}
	if !strings.Contains(paths, "$['project.name']") {
		t.Errorf("expected bracket notation $['project.name'] in jsonpaths, got: %s", paths)
	}
	if !strings.Contains(cols, "`project.id`") {
		t.Errorf("expected backtick-quoted `project.id` in columns, got: %s", cols)
	}
}

func TestBuildJSONMappings_SortedOutput(t *testing.T) {
	// Keys across multiple entities must be collected and sorted so the mapping
	// headers are deterministic across calls.
	entities := []map[string]any{
		{"z.key": "last"},
		{"a.key": "first"},
	}
	paths, cols := buildJSONMappings(entities)

	pathIdx := strings.Index(paths, "a.key")
	zPathIdx := strings.Index(paths, "z.key")
	if pathIdx > zPathIdx {
		t.Errorf("jsonpaths not sorted alphabetically: %s", paths)
	}

	colIdx := strings.Index(cols, "a.key")
	zColIdx := strings.Index(cols, "z.key")
	if colIdx > zColIdx {
		t.Errorf("columns not sorted alphabetically: %s", cols)
	}
}

func TestBuildJSONMappings_PathsAndColumnsAligned(t *testing.T) {
	// jsonpaths and columns must contain the same keys in the same order so
	// Doris maps extracted values to the correct columns.
	entities := []map[string]any{{"workflow_status.name": "RELEASED", "id": float64(1)}}
	paths, cols := buildJSONMappings(entities)

	// Unmarshal the JSON array to check length and order.
	var pathList []string
	if err := json.Unmarshal([]byte(paths), &pathList); err != nil {
		t.Fatalf("jsonpaths is not a valid JSON array: %v — value: %s", err, paths)
	}
	colList := strings.Split(cols, ",")

	if len(pathList) != len(colList) {
		t.Errorf("jsonpaths length %d != columns length %d", len(pathList), len(colList))
	}
}

func TestBuildJSONMappings_EmptyEntities(t *testing.T) {
	paths, cols := buildJSONMappings([]map[string]any{})
	if paths != "[]" {
		t.Errorf("expected empty JSON array for jsonpaths, got: %s", paths)
	}
	if cols != "" {
		t.Errorf("expected empty string for columns, got: %s", cols)
	}
}

// ─── normalizeEntityKeys ──────────────────────────────────────────────────────

func TestNormalizeEntityKeys_LowercasesKeys(t *testing.T) {
	entities := []map[string]any{
		{"Id": float64(1), "Name": "alice", "project.ID": float64(15)},
	}
	got := normalizeEntityKeys(entities)
	if _, ok := got[0]["id"]; !ok {
		t.Error("expected key 'id' after normalisation")
	}
	if _, ok := got[0]["name"]; !ok {
		t.Error("expected key 'name' after normalisation")
	}
	if _, ok := got[0]["project.id"]; !ok {
		t.Error("expected key 'project.id' after normalisation")
	}
}

func TestNormalizeEntityKeys_CaseCollisionWithinEntity(t *testing.T) {
	// Mirrors the real ERMA2 situation: the same logical field arrives under
	// two case variants in one entity. After normalisation only one key must
	// remain; Doris would reject both with "Duplicate column".
	entities := []map[string]any{
		{"fieldvalues.Cause": "engine", "fieldvalues.cause": "engine"},
	}
	got := normalizeEntityKeys(entities)
	if len(got[0]) != 1 {
		t.Errorf("expected 1 key after collision normalisation, got %d: %v", len(got[0]), got[0])
	}
	if _, ok := got[0]["fieldvalues.cause"]; !ok {
		t.Error("expected normalised key 'fieldvalues.cause'")
	}
}

func TestNormalizeEntityKeys_AlreadyLowercaseUnchanged(t *testing.T) {
	entities := []map[string]any{
		{"id": float64(1), "project.name": "MyProject"},
	}
	got := normalizeEntityKeys(entities)
	if got[0]["id"] != float64(1) || got[0]["project.name"] != "MyProject" {
		t.Errorf("already-lowercase entity should be unchanged: %v", got[0])
	}
}

func TestNormalizeEntityKeys_DoesNotMutateInput(t *testing.T) {
	original := []map[string]any{{"KEY": "value"}}
	_ = normalizeEntityKeys(original)
	if _, ok := original[0]["KEY"]; !ok {
		t.Error("normalizeEntityKeys must not mutate the input slice")
	}
}

func TestLoadPage_BasicAuthSent(t *testing.T) {
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": "Success"})
	}))
	defer srv.Close()

	loader := NewStreamLoader(config.DorisConfig{
		Host: srv.URL, Database: "db", Table: "t",
		User: "doris_user", Password: "doris_pass",
	})
	_ = loader.LoadPage(context.Background(), []map[string]any{{"k": "v"}})

	if user != "doris_user" || pass != "doris_pass" {
		t.Errorf("expected doris_user/doris_pass, got %q/%q", user, pass)
	}
}

func TestLoadPage_DorisFailureStatus_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Status":  "Fail",
			"Message": "schema mismatch",
		})
	}))
	defer srv.Close()

	loader := testLoader(srv.URL)
	err := loader.LoadPage(context.Background(), []map[string]any{{"k": "v"}})
	if err == nil {
		t.Fatal("expected error when Doris returns Status=Fail")
	}
}

func TestLoadPage_HTTP5xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	loader := testLoader(srv.URL)
	err := loader.LoadPage(context.Background(), []map[string]any{{"k": "v"}})
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
}

func TestLoadPage_PublishTimeout_TreatedAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": "Publish Timeout"})
	}))
	defer srv.Close()

	loader := testLoader(srv.URL)
	err := loader.LoadPage(context.Background(), []map[string]any{{"k": "v"}})
	if err != nil {
		t.Fatalf("Publish Timeout should be treated as success, got error: %v", err)
	}
}

func TestEncodeNDJSON(t *testing.T) {
	entities := []map[string]any{
		{"a": 1},
		{"b": 2},
	}
	data, err := encodeNDJSON(entities)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}
