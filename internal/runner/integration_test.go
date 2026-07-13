// integration_test.go exercises the full ingestion pipeline end-to-end through
// the Runner, wiring the source endpoint and the Doris Stream Load endpoint to
// in-process httptest mock servers. Each test models a "common RESTful setup"
// (pagination style, injection mode, auth scheme, flattening behaviour) so the
// whole auth → fetch → paginate → flatten → load path is covered without any
// external dependency.
//
// The param names used in several tests mirror real APIs this loader targets:
//   - SonarQube:  ?p=<page>&ps=<pageSize>, total under "paging.total"
//   - Polarion:   JSON:API page[number]/page[size], total under "meta.totalCount"
//   - Jira:       offset via ?startAt=&maxResults=, total under "total"
//   - Jenkins:    JSON API returning an object/array at the response root
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// ─── Doris mock ───────────────────────────────────────────────────────────────

// dorisState records what the mock Doris Stream Load endpoint received across
// all Stream Load transactions. It is safe for concurrent use because the
// runner streams pages from multiple goroutines.
type dorisState struct {
	mu    sync.Mutex
	calls int
	rows  []map[string]any
}

func (d *dorisState) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *dorisState) rowCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.rows)
}

// snapshotRows returns a copy of every ingested row for assertion.
func (d *dorisState) snapshotRows() []map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]map[string]any, len(d.rows))
	copy(out, d.rows)
	return out
}

// newDorisServer returns a mock Doris FE that accepts Stream Load PUTs, records
// the decoded NDJSON rows into state, and always replies with Status: Success.
func newDorisServer(t *testing.T, state *dorisState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("doris mock: expected PUT, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/_stream_load") {
			t.Errorf("doris mock: unexpected path %q", r.URL.Path)
		}
		dec := json.NewDecoder(r.Body)
		state.mu.Lock()
		state.calls++
		for {
			var obj map[string]any
			if err := dec.Decode(&obj); err != nil {
				break
			}
			state.rows = append(state.rows, obj)
		}
		state.mu.Unlock()
		writeJSON(w, map[string]any{"Status": "Success", "NumberLoadedRows": 1})
	}))
}

// newFailingDorisServer returns a mock Doris FE that replies HTTP 200 but with a
// Stream Load "Fail" status, which the loader must surface as an error.
func newFailingDorisServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"Status": "Fail", "Message": "simulated load failure"})
	}))
}

// ─── Config builder ─────────────────────────────────────────────────────────

// baseConfig returns a minimal, valid Config pointing at the given source and
// Doris mock URLs. Individual tests override the Source/Auth/Pagination fields
// for the specific RESTful setup under test. Retry defaults to a single attempt
// so tests fail fast unless they explicitly opt into retries.
func baseConfig(sourceURL, dorisURL string) *config.Config {
	enabled := true
	return &config.Config{
		Source: config.SourceConfig{URL: sourceURL, Method: "GET"},
		Auth:   config.AuthConfig{Type: "noauth"},
		Threading: config.ThreadingConfig{
			MaxGoroutines: 4,
			Retry:         config.RetryConfig{MaxAttempts: 1},
		},
		Flattening: config.FlatteningConfig{Enabled: &enabled, Separator: "."},
		Doris:      config.DorisConfig{Host: dorisURL, Database: "bronze", Table: "t", User: "root"},
	}
}

// runIngestion builds a Runner from cfg and executes a full Run.
func runIngestion(t *testing.T, cfg *config.Config) error {
	t.Helper()
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}
	return r.Run(context.Background())
}

// ─── small utilities ──────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// ─── 1. page_number + query_params (SonarQube-style: ?p=&ps=) ────────────────

func TestIntegration_PageNumber_QueryParams(t *testing.T) {
	const totalPages = 3
	const pageSize = 2

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var seenPages sync.Map // page number → struct{}
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := atoiDefault(r.URL.Query().Get("p"), 1)
		seenPages.Store(page, struct{}{})
		items := []map[string]any{
			{"id": float64(page*10 + 1)},
			{"id": float64(page*10 + 2)},
		}
		writeJSON(w, map[string]any{
			"paging": map[string]any{"total": float64(totalPages)},
			"items":  items,
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Pagination = config.PaginationConfig{
		Type:         "page_number",
		Injection:    "query_params",
		StartPage:    1,
		PageParam:    "p",
		LimitParam:   "ps",
		PageSize:     pageSize,
		NumPagesPath: "paging.total",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if got := state.callCount(); got != totalPages {
		t.Errorf("expected %d Doris calls (one per page), got %d", totalPages, got)
	}
	if got := state.rowCount(); got != totalPages*2 {
		t.Errorf("expected %d rows, got %d", totalPages*2, got)
	}
	for p := 1; p <= totalPages; p++ {
		if _, ok := seenPages.Load(p); !ok {
			t.Errorf("source was never asked for page %d", p)
		}
	}
}

// ─── 2. offset + query_params (Jira-style: ?startAt=&maxResults=) ────────────

func TestIntegration_Offset_QueryParams_Jira(t *testing.T) {
	const pageSize = 2
	// Five issues total → ceil(5/2) = 3 pages (startAt 0, 2, 4).
	dataset := []map[string]any{
		{"id": float64(1)}, {"id": float64(2)}, {"id": float64(3)},
		{"id": float64(4)}, {"id": float64(5)},
	}

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var mu sync.Mutex
	var seenStartAt []int
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startAt := atoiDefault(r.URL.Query().Get("startAt"), 0)
		maxResults := atoiDefault(r.URL.Query().Get("maxResults"), pageSize)
		mu.Lock()
		seenStartAt = append(seenStartAt, startAt)
		mu.Unlock()

		end := startAt + maxResults
		if end > len(dataset) {
			end = len(dataset)
		}
		var slice []map[string]any
		if startAt < len(dataset) {
			slice = dataset[startAt:end]
		}
		writeJSON(w, map[string]any{
			"total":  float64(len(dataset)),
			"issues": slice,
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "issues"
	cfg.Pagination = config.PaginationConfig{
		Type:             "offset",
		Injection:        "query_params",
		StartPage:        1,
		OffsetParam:      "startAt",
		LimitParam:       "maxResults",
		PageSize:         pageSize,
		TotalEntriesPath: "total",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if got := state.callCount(); got != 3 {
		t.Errorf("expected 3 Doris calls, got %d", got)
	}
	if got := state.rowCount(); got != len(dataset) {
		t.Errorf("expected %d rows, got %d", len(dataset), got)
	}
	mu.Lock()
	defer mu.Unlock()
	want := map[int]bool{0: true, 2: true, 4: true}
	for _, s := range seenStartAt {
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("expected offsets {0,2,4} to all be requested; missing: %v (saw %v)", want, seenStartAt)
	}
}

// ─── 3. offset + path injection ({offset}/{limit}) ───────────────────────────

func TestIntegration_Offset_PathInjection(t *testing.T) {
	const pageSize = 2
	// Four entities → 2 pages (offset 0, 2).
	dataset := []map[string]any{
		{"id": float64(1)}, {"id": float64(2)},
		{"id": float64(3)}, {"id": float64(4)},
	}

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path pattern: /{offset}/{limit}
		var offset, limit int
		fmt.Sscanf(r.URL.Path, "/%d/%d", &offset, &limit)
		if limit == 0 {
			limit = pageSize
		}
		end := offset + limit
		if end > len(dataset) {
			end = len(dataset)
		}
		var slice []map[string]any
		if offset < len(dataset) {
			slice = dataset[offset:end]
		}
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"total": float64(len(dataset))},
			"items": slice,
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL+"/{offset}/{limit}", dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Pagination = config.PaginationConfig{
		Type:             "offset",
		Injection:        "path",
		StartPage:        1,
		OffsetParam:      "offset",
		LimitParam:       "limit",
		PageSize:         pageSize,
		TotalEntriesPath: "meta.total",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if got := state.callCount(); got != 2 {
		t.Errorf("expected 2 Doris calls, got %d", got)
	}
	if got := state.rowCount(); got != len(dataset) {
		t.Errorf("expected %d rows, got %d", len(dataset), got)
	}
}

// ─── 4. page_number, page count derived from total_entries_path (Polarion) ───

func TestIntegration_PageNumber_TotalEntriesDerivation(t *testing.T) {
	const pageSize = 2
	const totalEntries = 5 // ceil(5/2) = 3 pages

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Polarion JSON:API style params: page[number] / page[size].
		page := atoiDefault(r.URL.Query().Get("page[number]"), 1)
		items := []map[string]any{
			{"id": float64(page*10 + 1)},
			{"id": float64(page*10 + 2)},
		}
		writeJSON(w, map[string]any{
			"meta": map[string]any{"totalCount": float64(totalEntries)},
			"data": items,
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "data"
	cfg.Pagination = config.PaginationConfig{
		Type:             "page_number",
		Injection:        "query_params",
		StartPage:        1,
		PageParam:        "page[number]",
		LimitParam:       "page[size]",
		PageSize:         pageSize,
		TotalEntriesPath: "meta.totalCount",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// ceil(5 / 2) = 3 pages.
	if got := state.callCount(); got != 3 {
		t.Errorf("expected 3 Doris calls (ceil(5/2)), got %d", got)
	}
}

// ─── 5. nested data_path extraction (data.items) ─────────────────────────────

func TestIntegration_NestedDataPath(t *testing.T) {
	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	// Single page; entities live two levels deep under data.items.
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"meta": map[string]any{"totalPages": float64(1)},
			"data": map[string]any{
				"items": []map[string]any{
					{"id": float64(1)},
					{"id": float64(2)},
					{"id": float64(3)},
				},
			},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "data.items"
	cfg.Pagination = config.PaginationConfig{
		Type:         "page_number",
		Injection:    "query_params",
		StartPage:    1,
		PageParam:    "page",
		LimitParam:   "limit",
		PageSize:     10,
		NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if got := state.callCount(); got != 1 {
		t.Errorf("expected 1 Doris call, got %d", got)
	}
	if got := state.rowCount(); got != 3 {
		t.Errorf("expected 3 rows from data.items, got %d", got)
	}
}

// ─── 6. basic auth forwarded to the source ───────────────────────────────────

func TestIntegration_BasicAuth(t *testing.T) {
	const user, pass = "svc_user", "s3cr3t"

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var gotUser, gotPass string
	var gotOK bool
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		if !gotOK || gotUser != user || gotPass != pass {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{{"id": float64(1)}},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Auth = config.AuthConfig{Type: "basic", Username: user, Password: pass}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !gotOK || gotUser != user || gotPass != pass {
		t.Errorf("source did not receive expected basic auth (ok=%v user=%q)", gotOK, gotUser)
	}
	if got := state.rowCount(); got != 1 {
		t.Errorf("expected 1 row, got %d", got)
	}
}

// ─── 7. static bearer token forwarded to the source ─────────────────────────

func TestIntegration_BearerAuth(t *testing.T) {
	const token = "static-token-123"

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var gotAuth string
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{{"id": float64(1)}},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Auth = config.AuthConfig{Type: "bearer", Token: token}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("expected Authorization %q, got %q", "Bearer "+token, gotAuth)
	}
}

// ─── 8. preflight_bearer: login → token → attached to data requests ──────────

func TestIntegration_PreflightBearerAuth(t *testing.T) {
	const token = "preflight-issued-token"

	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	// Auth endpoint: accepts {"username","password"} and returns an access_token.
	var loginCalls int
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		if r.Method != http.MethodPost {
			t.Errorf("auth mock: expected POST, got %s", r.Method)
		}
		var creds map[string]string
		_ = json.NewDecoder(r.Body).Decode(&creds)
		if creds["username"] != "u" || creds["password"] != "p" {
			http.Error(w, "bad creds", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"access_token": token})
	}))
	defer authSrv.Close()

	var gotAuth string
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{{"id": float64(1)}},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Auth = config.AuthConfig{Type: "preflight_bearer", URL: authSrv.URL, Username: "u", Password: "p"}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("expected exactly 1 preflight login call, got %d", loginCalls)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("expected data request Authorization %q, got %q", "Bearer "+token, gotAuth)
	}
}

// ─── 9. nested-object flattening end-to-end ──────────────────────────────────

func TestIntegration_NestedFlattening(t *testing.T) {
	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"meta": map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{
				{"id": float64(1), "author": map[string]any{"name": "alice"}},
			},
		})
	}))
	defer sourceSrv.Close()

	enabled := true
	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Flattening = config.FlatteningConfig{Enabled: &enabled, Separator: "."}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	rows := state.snapshotRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if _, ok := rows[0]["author.name"]; !ok {
		t.Errorf("expected flattened key 'author.name', got keys %v", keysOf(rows[0]))
	}
	if _, ok := rows[0]["author"]; ok {
		t.Errorf("nested 'author' object should not survive flattening, got keys %v", keysOf(rows[0]))
	}
}

// ─── 10. max_depth flattening keeps deep objects as JSON leaves ──────────────

func TestIntegration_MaxDepthFlattening(t *testing.T) {
	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"meta": map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{
				{"project": map[string]any{
					"id":   float64(1),
					"meta": map[string]any{"color": "red"},
				}},
			},
		})
	}))
	defer sourceSrv.Close()

	enabled := true
	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Flattening = config.FlatteningConfig{Enabled: &enabled, Separator: ".", MaxDepth: 1}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	rows := state.snapshotRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// At max_depth=1 the "project" object is preserved as a JSON leaf, so it
	// round-trips through NDJSON back into a nested map — never project.id.
	if _, ok := rows[0]["project"]; !ok {
		t.Errorf("expected 'project' kept as a leaf at max_depth=1, got keys %v", keysOf(rows[0]))
	}
	if _, ok := rows[0]["project.id"]; ok {
		t.Errorf("project.id should not be flattened at max_depth=1, got keys %v", keysOf(rows[0]))
	}
}

// keysOf returns the keys of a row for readable assertion failure messages.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ─── 11. transient source failure is retried, then succeeds ──────────────────

func TestIntegration_RetryOnTransientFailure(t *testing.T) {
	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var attempts int32
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Fail the very first attempt with 503, then serve data.
		if atomic.AddInt32(&attempts, 1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{{"id": float64(1)}},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Threading.Retry = config.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: config.Duration{Duration: time.Millisecond},
		Multiplier:   2.0,
		MaxDelay:     config.Duration{Duration: 10 * time.Millisecond},
	}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err != nil {
		t.Fatalf("Run should have succeeded after a retry, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Errorf("expected at least 2 source attempts (1 fail + 1 success), got %d", got)
	}
	if got := state.rowCount(); got != 1 {
		t.Errorf("expected 1 row after successful retry, got %d", got)
	}
}

// ─── 12. Doris Stream Load "Fail" status surfaces as an error ────────────────

func TestIntegration_DorisFailurePropagates(t *testing.T) {
	dorisSrv := newFailingDorisServer(t)
	defer dorisSrv.Close()

	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"meta":  map[string]any{"totalPages": float64(1)},
			"items": []map[string]any{{"id": float64(1)}},
		})
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err == nil {
		t.Fatal("expected an error when Doris reports Status: Fail, got nil")
	}
}

// ─── 13. persistent source failure exhausts retries and errors out ───────────

func TestIntegration_SourceFailureExhaustsRetries(t *testing.T) {
	state := &dorisState{}
	dorisSrv := newDorisServer(t, state)
	defer dorisSrv.Close()

	var attempts int32
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer sourceSrv.Close()

	cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
	cfg.Source.DataPath = "items"
	cfg.Threading.Retry = config.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: config.Duration{Duration: time.Millisecond},
		Multiplier:   2.0,
		MaxDelay:     config.Duration{Duration: 10 * time.Millisecond},
	}
	cfg.Pagination = config.PaginationConfig{
		Type: "page_number", Injection: "query_params", StartPage: 1,
		PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
	}

	if err := runIngestion(t, cfg); err == nil {
		t.Fatal("expected an error when the source always fails, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Errorf("expected retries to produce multiple source attempts, got %d", got)
	}
	if got := state.rowCount(); got != 0 {
		t.Errorf("expected no rows loaded on persistent failure, got %d", got)
	}
}
