package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// buildConfig creates a minimal Config pointing at two test servers.
func buildConfig(sourceURL, dorisURL string, totalPages int, numPagePath string) *config.Config {
	enabled := true
	startPage := 1
	return &config.Config{
		Source: config.SourceConfig{
			URL:      sourceURL + "/{page}/{limit}",
			Method:   "GET",
			DataPath: "items",
		},
		Auth: config.AuthConfig{Type: "noauth"},
		Pagination: config.PaginationConfig{
			Type:         "page_number",
			Injection:    "path",
			StartPage:    startPage,
			PageParam:    "page",
			LimitParam:   "limit",
			PageSize:     2,
			NumPagesPath: numPagePath,
		},
		Threading: config.ThreadingConfig{
			MaxGoroutines: 3,
			Retry:         config.RetryConfig{MaxAttempts: 1},
		},
		Flattening: config.FlatteningConfig{
			Enabled:   &enabled,
			Separator: ".",
		},
		Doris: config.DorisConfig{
			Host:     dorisURL,
			Database: "testdb",
			Table:    "testtable",
			User:     "root",
		},
	}
}

// pageResponse produces a standard paginated response body.
func pageResponse(pageNum, totalPages int, items []map[string]any) map[string]any {
	return map[string]any{
		"meta":  map[string]any{"totalPages": float64(totalPages)},
		"items": items,
	}
}

func TestRunner_SinglePage(t *testing.T) {
	var dorisCallCount int32

	// Mock Doris Stream Load endpoint.
	dorisSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&dorisCallCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": "Success"})
	}))
	defer dorisSrv.Close()

	// Mock source: single page (totalPages=1).
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pageResponse(1, 1, []map[string]any{
			{"id": float64(1)},
			{"id": float64(2)},
		}))
	}))
	defer sourceSrv.Close()

	cfg := buildConfig(sourceSrv.URL, dorisSrv.URL, 1, "meta.totalPages")
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if atomic.LoadInt32(&dorisCallCount) != 1 {
		t.Errorf("expected 1 Doris call, got %d", atomic.LoadInt32(&dorisCallCount))
	}
}

func TestRunner_MultiplePages(t *testing.T) {
	var dorisCallCount int32
	var dorisRowCount int32

	dorisSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dorisCallCount, 1)
		// Count lines in the NDJSON body to track total entity ingestion.
		dec := json.NewDecoder(r.Body)
		for {
			var obj map[string]any
			if err := dec.Decode(&obj); err != nil {
				break
			}
			atomic.AddInt32(&dorisRowCount, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": "Success"})
	}))
	defer dorisSrv.Close()

	// Source serves 3 pages, each with 2 items.
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Page number is the second path segment after the server URL.
		// URL pattern: /{page}/{limit}
		var pageNum int
		fmt.Sscanf(r.URL.Path, "/%d/", &pageNum)
		if pageNum == 0 {
			pageNum = 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pageResponse(pageNum, 3, []map[string]any{
			{"id": float64(pageNum*10 + 1)},
			{"id": float64(pageNum*10 + 2)},
		}))
	}))
	defer sourceSrv.Close()

	cfg := buildConfig(sourceSrv.URL, dorisSrv.URL, 3, "meta.totalPages")
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if atomic.LoadInt32(&dorisCallCount) != 3 {
		t.Errorf("expected 3 Doris calls (one per page), got %d", atomic.LoadInt32(&dorisCallCount))
	}
	if atomic.LoadInt32(&dorisRowCount) != 6 {
		t.Errorf("expected 6 rows total (3 pages × 2 items), got %d", atomic.LoadInt32(&dorisRowCount))
	}
}

func TestRunner_ArrayAtRoot_SinglePage(t *testing.T) {
	var dorisCallCount int32

	dorisSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&dorisCallCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": "Success"})
	}))
	defer dorisSrv.Close()

	// Source returns an array at root — no pagination metadata.
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": float64(1)}, {"id": float64(2)}})
	}))
	defer sourceSrv.Close()

	// Use empty data_path so the root array is used directly.
	enabled := true
	cfg := &config.Config{
		Source: config.SourceConfig{
			URL:      sourceSrv.URL + "/{page}/{limit}",
			Method:   "GET",
			DataPath: "", // root
		},
		Auth: config.AuthConfig{Type: "noauth"},
		Pagination: config.PaginationConfig{
			Type:         "page_number",
			Injection:    "path",
			StartPage:    1,
			PageParam:    "page",
			LimitParam:   "limit",
			PageSize:     10,
			NumPagesPath: "meta.pages",
		},
		Threading:  config.ThreadingConfig{MaxGoroutines: 2, Retry: config.RetryConfig{MaxAttempts: 1}},
		Flattening: config.FlatteningConfig{Enabled: &enabled, Separator: "."},
		Doris:      config.DorisConfig{Host: dorisSrv.URL, Database: "db", Table: "t", User: "root"},
	}

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if atomic.LoadInt32(&dorisCallCount) != 1 {
		t.Errorf("expected 1 Doris call for array-at-root response, got %d", atomic.LoadInt32(&dorisCallCount))
	}
}

func TestExtractEntities_ObjectRoot_NoDataPath(t *testing.T) {
	raw := map[string]any{"id": float64(1), "name": "alice"}
	entities, err := extractEntities(raw, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 {
		t.Errorf("expected 1 entity for object root, got %d", len(entities))
	}
}

func TestExtractEntities_ArrayRoot_NoDataPath(t *testing.T) {
	raw := []any{
		map[string]any{"id": float64(1)},
		map[string]any{"id": float64(2)},
	}
	entities, err := extractEntities(raw, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(entities))
	}
}

func TestExtractEntities_DataPath(t *testing.T) {
	raw := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{"id": float64(10)},
			},
		},
	}
	entities, err := extractEntities(raw, "data.items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 1 {
		t.Errorf("expected 1 entity, got %d", len(entities))
	}
	if entities[0]["id"] != float64(10) {
		t.Errorf("unexpected entity value: %v", entities[0])
	}
}

func TestExtractEntities_MissingDataPath_ReturnsError(t *testing.T) {
	raw := map[string]any{"other": "value"}
	_, err := extractEntities(raw, "data.missing")
	if err == nil {
		t.Fatal("expected error for missing data_path")
	}
}
