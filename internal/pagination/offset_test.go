package pagination

import (
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func defaultOffsetCfg() config.PaginationConfig {
	return config.PaginationConfig{
		Type:             "offset",
		Injection:        "path",
		PageSize:         100,
		OffsetParam:      "offset",
		LimitParam:       "limit",
		TotalEntriesPath: "meta.total",
	}
}

func TestOffset_Evaluate(t *testing.T) {
	cfg := defaultOffsetCfg()
	adapter := newOffset(cfg)

	resp := map[string]any{
		"meta": map[string]any{"total": float64(250)},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	// ceil(250 / 100) = 3
	if info.TotalPages != 3 {
		t.Errorf("TotalPages: expected 3, got %d", info.TotalPages)
	}
	if info.TotalEntries != 250 {
		t.Errorf("TotalEntries: expected 250, got %d", info.TotalEntries)
	}
	if info.StartPage != 1 {
		t.Errorf("StartPage: expected 1, got %d", info.StartPage)
	}
}

func TestOffset_Evaluate_ExactMultiple(t *testing.T) {
	cfg := defaultOffsetCfg()
	adapter := newOffset(cfg)

	resp := map[string]any{
		"meta": map[string]any{"total": float64(300)},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	if info.TotalPages != 3 {
		t.Errorf("TotalPages: expected 3, got %d", info.TotalPages)
	}
}

func TestOffset_Evaluate_ZeroEntries(t *testing.T) {
	cfg := defaultOffsetCfg()
	adapter := newOffset(cfg)

	resp := map[string]any{
		"meta": map[string]any{"total": float64(0)},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	if info.TotalPages != 0 {
		t.Errorf("expected 0 total pages for empty result")
	}
}

func TestOffset_BuildURL_PageOne(t *testing.T) {
	cfg := defaultOffsetCfg()
	adapter := newOffset(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items/{offset}/{limit}", 1)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 1 → offset = 0
	want := "https://api.example.com/items/0/100"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_PageThree(t *testing.T) {
	cfg := defaultOffsetCfg()
	adapter := newOffset(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items/{offset}/{limit}", 3)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 3 → offset = (3-1)*100 = 200
	want := "https://api.example.com/items/200/100"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_QueryParamsCleanURL(t *testing.T) {
	cfg := defaultOffsetCfg()
	cfg.Injection = "query_params"
	adapter := newOffset(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items", 3)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 3 → offset = (3-1)*100 = 200; url.Values.Encode() sorts keys alphabetically.
	want := "https://api.example.com/items?limit=100&offset=200"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_QueryParamsPreservesExistingParams(t *testing.T) {
	cfg := defaultOffsetCfg()
	cfg.Injection = "query_params"
	adapter := newOffset(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items?filter=active&sort=name", 2)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 2 → offset = 100; existing params are preserved and keys sorted.
	want := "https://api.example.com/items?filter=active&limit=100&offset=100&sort=name"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_QueryParamsOverwritesExistingOffsetParam(t *testing.T) {
	cfg := defaultOffsetCfg()
	cfg.Injection = "query_params"
	adapter := newOffset(cfg)

	// Base URL already carries a stale offset value — injector must overwrite it.
	got, err := adapter.BuildURL("https://api.example.com/items?offset=0", 4)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 4 → offset = (4-1)*100 = 300
	want := "https://api.example.com/items?limit=100&offset=300"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_QueryParamsJiraStyle(t *testing.T) {
	// Reflects the Jira /search use case: startAt/maxResults query params.
	cfg := defaultOffsetCfg()
	cfg.Injection = "query_params"
	cfg.PageSize = 50
	cfg.OffsetParam = "startAt"
	cfg.LimitParam = "maxResults"
	adapter := newOffset(cfg)

	got, err := adapter.BuildURL("https://jira.example.com/api/2/search", 3)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// page 3 → startAt = (3-1)*50 = 100
	want := "https://jira.example.com/api/2/search?maxResults=50&startAt=100"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestOffset_BuildURL_BodyNotImplemented(t *testing.T) {
	cfg := defaultOffsetCfg()
	cfg.Injection = "body"
	adapter := newOffset(cfg)

	_, err := adapter.BuildURL("https://api.example.com/items", 2)
	if err == nil {
		t.Fatal("expected error for unimplemented body injection")
	}
}
