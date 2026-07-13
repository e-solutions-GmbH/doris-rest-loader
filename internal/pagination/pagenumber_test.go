package pagination

import (
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func defaultPageNumberCfg() config.PaginationConfig {
	return config.PaginationConfig{
		Type:             "page_number",
		Injection:        "path",
		StartPage:        1,
		PageParam:        "page",
		LimitParam:       "limit",
		PageSize:         100,
		NumPagesPath:     "meta.totalPages",
		TotalEntriesPath: "meta.totalCount",
	}
}

func TestPageNumber_Evaluate_NumPagesPath(t *testing.T) {
	cfg := defaultPageNumberCfg()
	adapter := newPageNumber(cfg)

	resp := map[string]any{
		"meta": map[string]any{
			"totalPages": float64(7),
		},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	if info.TotalPages != 7 {
		t.Errorf("TotalPages: expected 7, got %d", info.TotalPages)
	}
	if info.StartPage != 1 {
		t.Errorf("StartPage: expected 1, got %d", info.StartPage)
	}
	if info.PageSize != 100 {
		t.Errorf("PageSize: expected 100, got %d", info.PageSize)
	}
}

func TestPageNumber_Evaluate_TotalEntriesPath(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.NumPagesPath = "" // disable num_pages_path; fall back to total entries
	adapter := newPageNumber(cfg)

	resp := map[string]any{
		"meta": map[string]any{
			"totalCount": float64(350),
		},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	// ceil(350 / 100) = 4
	if info.TotalPages != 4 {
		t.Errorf("TotalPages: expected 4, got %d", info.TotalPages)
	}
	if info.TotalEntries != 350 {
		t.Errorf("TotalEntries: expected 350, got %d", info.TotalEntries)
	}
}

func TestPageNumber_Evaluate_ZeroEntries(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.NumPagesPath = ""
	adapter := newPageNumber(cfg)

	resp := map[string]any{
		"meta": map[string]any{"totalCount": float64(0)},
	}

	info, err := adapter.Evaluate(resp)
	if err != nil {
		t.Fatalf("Evaluate returned unexpected error: %v", err)
	}
	if info.TotalPages != 0 {
		t.Errorf("TotalPages: expected 0 for empty result, got %d", info.TotalPages)
	}
}

func TestPageNumber_Evaluate_MissingPath(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.NumPagesPath = "meta.doesNotExist"
	adapter := newPageNumber(cfg)

	resp := map[string]any{"meta": map[string]any{}}
	_, err := adapter.Evaluate(resp)
	if err == nil {
		t.Fatal("expected error for missing num_pages_path key")
	}
}

func TestPageNumber_BuildURL_PathInjection(t *testing.T) {
	cfg := defaultPageNumberCfg()
	adapter := newPageNumber(cfg)

	got, err := adapter.BuildURL("https://api.example.com/v1/items/{page}/{limit}", 3)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/v1/items/3/100"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestPageNumber_BuildURL_FirstPage(t *testing.T) {
	cfg := defaultPageNumberCfg()
	adapter := newPageNumber(cfg)

	got, err := adapter.BuildURL("https://api.example.com/{page}/{limit}", 1)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/1/100"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestPageNumber_BuildURL_QueryParamsCleanURL(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.Injection = "query_params"
	adapter := newPageNumber(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items", 3)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/items?limit=100&page=3"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestPageNumber_BuildURL_QueryParamsPreservesExistingParams(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.Injection = "query_params"
	adapter := newPageNumber(cfg)

	got, err := adapter.BuildURL("https://api.example.com/items?filter=active&sort=name", 2)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	// url.Values.Encode() sorts keys alphabetically.
	want := "https://api.example.com/items?filter=active&limit=100&page=2&sort=name"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestPageNumber_BuildURL_QueryParamsOverwritesExistingPageParam(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.Injection = "query_params"
	adapter := newPageNumber(cfg)

	// Base URL already carries a stale page value — injector must overwrite it.
	got, err := adapter.BuildURL("https://api.example.com/items?page=1", 5)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/items?limit=100&page=5"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestPageNumber_BuildURL_BodyNotImplemented(t *testing.T) {
	cfg := defaultPageNumberCfg()
	cfg.Injection = "body"
	adapter := newPageNumber(cfg)

	_, err := adapter.BuildURL("https://api.example.com/items", 2)
	if err == nil {
		t.Fatal("expected error for unimplemented body injection")
	}
}
