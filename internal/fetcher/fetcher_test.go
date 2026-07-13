package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/auth"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func sourceCfgFor(url string) config.SourceConfig {
	return config.SourceConfig{Method: "GET", URL: url}
}

func onceRetryCfg() config.RetryConfig {
	return config.RetryConfig{MaxAttempts: 1}
}

func TestFetcher_SuccessObjectResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "test"})
	}))
	defer srv.Close()

	f := New(sourceCfgFor(srv.URL), onceRetryCfg(), &auth.NoAuth{})
	result, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any response, got %T", result)
	}
	if obj["name"] != "test" {
		t.Errorf("expected name=test, got %v", obj["name"])
	}
}

func TestFetcher_SuccessArrayResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1}, {"id": 2}})
	}))
	defer srv.Close()

	f := New(sourceCfgFor(srv.URL), onceRetryCfg(), &auth.NoAuth{})
	result, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any response, got %T", result)
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 elements, got %d", len(arr))
	}
}

func TestFetcher_StaticHeadersSent(t *testing.T) {
	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom-Header")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	src := config.SourceConfig{
		Method:  "GET",
		URL:     srv.URL,
		Headers: map[string]string{"X-Custom-Header": "hello"},
	}
	f := New(src, onceRetryCfg(), &auth.NoAuth{})
	_, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedHeader != "hello" {
		t.Errorf("expected X-Custom-Header=hello, got %q", receivedHeader)
	}
}

func TestFetcher_StaticQueryParamsMerged(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	src := config.SourceConfig{
		Method:      "GET",
		URL:         srv.URL,
		QueryParams: map[string]string{"format": "json"},
	}
	f := New(src, onceRetryCfg(), &auth.NoAuth{})
	_, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rawQuery != "format=json" {
		t.Errorf("expected query format=json, got %q", rawQuery)
	}
}

func TestFetcher_BearerAuthApplied(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	f := New(sourceCfgFor(srv.URL), onceRetryCfg(), &auth.BearerAuth{Token: "secret-token"})
	_, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAuth != "Bearer secret-token" {
		t.Errorf("expected 'Bearer secret-token', got %q", receivedAuth)
	}
}

func TestFetcher_Non2xxStatus_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	f := New(sourceCfgFor(srv.URL), onceRetryCfg(), &auth.NoAuth{})
	_, err := f.FetchWithRetry(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
