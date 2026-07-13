package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreflightBearer_Prepare_Success(t *testing.T) {
	// Mock auth server returns a token field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if body["username"] != "alice" || body["password"] != "secret" {
			http.Error(w, "wrong credentials", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "returned-bearer-token"})
	}))
	defer srv.Close()

	adapter := NewPreflightBearer(srv.URL, "alice", "secret", srv.Client())

	if err := adapter.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned unexpected error: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://api.example.com", nil)
	if err := adapter.Apply(req); err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer returned-bearer-token" {
		t.Errorf("expected 'Bearer returned-bearer-token', got %q", got)
	}
}

func TestPreflightBearer_Prepare_AccessTokenField(t *testing.T) {
	// Mock auth server returns an access_token field (OAuth2-style).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oauth-style-token"})
	}))
	defer srv.Close()

	adapter := NewPreflightBearer(srv.URL, "u", "p", srv.Client())
	if err := adapter.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned unexpected error: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://api.example.com", nil)
	_ = adapter.Apply(req)
	if got := req.Header.Get("Authorization"); got != "Bearer oauth-style-token" {
		t.Errorf("expected 'Bearer oauth-style-token', got %q", got)
	}
}

func TestPreflightBearer_Prepare_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	adapter := NewPreflightBearer(srv.URL, "u", "p", srv.Client())
	err := adapter.Prepare(context.Background())
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
}

func TestPreflightBearer_Prepare_NoTokenField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"something_else": "value"})
	}))
	defer srv.Close()

	adapter := NewPreflightBearer(srv.URL, "u", "p", srv.Client())
	err := adapter.Prepare(context.Background())
	if err == nil {
		t.Fatal("expected error when token field is absent from response")
	}
}

func TestPreflightBearer_Apply_BeforePrepare_ReturnsError(t *testing.T) {
	adapter := NewPreflightBearer("http://unused", "u", "p", http.DefaultClient)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	err := adapter.Apply(req)
	if err == nil {
		t.Fatal("expected error when Apply is called before Prepare")
	}
}
