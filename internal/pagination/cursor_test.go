package pagination

import (
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func defaultCursorCfg() config.PaginationConfig {
	return config.PaginationConfig{
		Type:         "cursor",
		Injection:    "query_params",
		StartPage:    1,
		PageSize:     1000,
		LimitParam:   "limit",
		CursorParam:  "start",
		CursorPath:   "nextPageStart",
		LastPagePath: "isLastPage",
	}
}

func TestCursor_Evaluate_NotSupported(t *testing.T) {
	adapter := newCursor(defaultCursorCfg())
	_, err := adapter.Evaluate(map[string]any{})
	if err == nil {
		t.Fatal("expected error: cursor pagination does not support Evaluate")
	}
}

func TestCursor_BuildURL_StartPage_QueryParams(t *testing.T) {
	cfg := defaultCursorCfg()
	adapter := newCursor(cfg)

	got, err := adapter.BuildURL("https://api.example.com/v1/items", cfg.StartPage)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/v1/items?limit=1000"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestCursor_BuildURL_StartPage_Path(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.Injection = "path"
	adapter := newCursor(cfg)

	got, err := adapter.BuildURL("https://api.example.com/v1/items/{limit}", cfg.StartPage)
	if err != nil {
		t.Fatalf("BuildURL returned unexpected error: %v", err)
	}
	want := "https://api.example.com/v1/items/1000"
	if got != want {
		t.Errorf("BuildURL: expected %q, got %q", want, got)
	}
}

func TestCursor_BuildURL_RejectsNonStartPage(t *testing.T) {
	cfg := defaultCursorCfg()
	adapter := newCursor(cfg)

	_, err := adapter.BuildURL("https://api.example.com/v1/items", 2)
	if err == nil {
		t.Fatal("expected error: cursor pagination cannot build an arbitrary page directly")
	}
}

func TestCursor_BuildURL_BodyNotImplemented(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.Injection = "body"
	adapter := newCursor(cfg)

	_, err := adapter.BuildURL("https://api.example.com/items", cfg.StartPage)
	if err == nil {
		t.Fatal("expected error for unimplemented body injection")
	}
}

func TestCursor_NextPageURL_LastPagePath_True_StopsPagination(t *testing.T) {
	cfg := defaultCursorCfg()
	adapter := newCursor(cfg)

	resp := map[string]any{"isLastPage": true, "nextPageStart": float64(100)}
	_, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasNext {
		t.Error("expected hasNext=false when isLastPage is true")
	}
}

func TestCursor_NextPageURL_LastPagePath_False_ContinuesPagination(t *testing.T) {
	cfg := defaultCursorCfg()
	adapter := newCursor(cfg)

	resp := map[string]any{"isLastPage": false, "nextPageStart": float64(1000)}
	got, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items?limit=1000", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNext {
		t.Fatal("expected hasNext=true when isLastPage is false")
	}
	want := "https://api.example.com/v1/items?limit=1000&start=1000"
	if got != want {
		t.Errorf("NextPageURL: expected %q, got %q", want, got)
	}
}

func TestCursor_NextPageURL_NoLastPagePath_CursorAbsent_StopsPagination(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.LastPagePath = "" // rely on cursor-value-absence as the termination signal
	adapter := newCursor(cfg)

	resp := map[string]any{"values": []any{}} // no "nextPageStart" field at all
	_, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasNext {
		t.Error("expected hasNext=false when cursor_path is absent and last_page_path is unset")
	}
}

func TestCursor_NextPageURL_NoLastPagePath_CursorPresent_ContinuesPagination(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.LastPagePath = ""
	adapter := newCursor(cfg)

	resp := map[string]any{"nextPageStart": float64(2000)}
	got, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items?limit=1000", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNext {
		t.Fatal("expected hasNext=true when cursor_path resolves to a value")
	}
	want := "https://api.example.com/v1/items?limit=1000&start=2000"
	if got != want {
		t.Errorf("NextPageURL: expected %q, got %q", want, got)
	}
}

func TestCursor_NextPageURL_StringCursor(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.CursorPath = "nextCursor"
	adapter := newCursor(cfg)

	resp := map[string]any{"isLastPage": false, "nextCursor": "opaque-token-abc"}
	got, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items?limit=1000", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNext {
		t.Fatal("expected hasNext=true")
	}
	want := "https://api.example.com/v1/items?limit=1000&start=opaque-token-abc"
	if got != want {
		t.Errorf("NextPageURL: expected %q, got %q", want, got)
	}
}

func TestCursor_NextPageURL_PathInjection(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.Injection = "path"
	adapter := newCursor(cfg)

	resp := map[string]any{"isLastPage": false, "nextPageStart": float64(1000)}
	got, hasNext, err := adapter.NextPageURL("https://api.example.com/v1/items/{start}/{limit}", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasNext {
		t.Fatal("expected hasNext=true")
	}
	want := "https://api.example.com/v1/items/1000/1000"
	if got != want {
		t.Errorf("NextPageURL: expected %q, got %q", want, got)
	}
}

func TestCursor_NextPageURL_BodyNotImplemented(t *testing.T) {
	cfg := defaultCursorCfg()
	cfg.Injection = "body"
	adapter := newCursor(cfg)

	resp := map[string]any{"isLastPage": false, "nextPageStart": float64(1000)}
	_, _, err := adapter.NextPageURL("https://api.example.com/items", resp)
	if err == nil {
		t.Fatal("expected error for unimplemented body injection")
	}
}

func TestCursor_ImplementsSequentialAdapter(t *testing.T) {
	var _ SequentialAdapter = newCursor(defaultCursorCfg())
}
