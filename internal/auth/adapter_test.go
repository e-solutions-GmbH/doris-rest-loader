package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func TestNoAuth_PrepareAndApply(t *testing.T) {
	a := &NoAuth{}

	if err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned unexpected error: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	if h := req.Header.Get("Authorization"); h != "" {
		t.Errorf("NoAuth should not set Authorization header, got %q", h)
	}
}

func TestBasicAuth_Apply(t *testing.T) {
	a := &BasicAuth{Username: "user", Password: "pass"}

	if err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned unexpected error: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	u, p, ok := req.BasicAuth()
	if !ok {
		t.Fatal("expected Basic Auth header to be present")
	}
	if u != "user" {
		t.Errorf("username: expected %q, got %q", "user", u)
	}
	if p != "pass" {
		t.Errorf("password: expected %q, got %q", "pass", p)
	}
}

func TestBearerAuth_Apply(t *testing.T) {
	a := &BearerAuth{Token: "my-secret-token"}

	if err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare returned unexpected error: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}

	want := "Bearer my-secret-token"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization header: expected %q, got %q", want, got)
	}
}

func TestBearerAuth_EmptyToken_ReturnsError(t *testing.T) {
	a := &BearerAuth{Token: ""}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	err := a.Apply(req)
	if err == nil {
		t.Fatal("expected an error when bearer token is empty")
	}
}

func TestNewAdapter_NoAuth(t *testing.T) {
	cfg := config.AuthConfig{Type: "noauth"}
	adapter, err := NewAdapter(cfg, false)
	if err != nil {
		t.Fatalf("NewAdapter returned unexpected error: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if err := adapter.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare on NoAuth returned error: %v", err)
	}
}

func TestNewAdapter_Basic(t *testing.T) {
	cfg := config.AuthConfig{Type: "basic", Username: "u", Password: "p"}
	adapter, err := NewAdapter(cfg, false)
	if err != nil {
		t.Fatalf("NewAdapter returned unexpected error: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_ = adapter.Apply(req)
	_, _, ok := req.BasicAuth()
	if !ok {
		t.Error("expected Basic Auth to be applied by adapter")
	}
}

func TestNewAdapter_Bearer(t *testing.T) {
	cfg := config.AuthConfig{Type: "bearer", Token: "tok123"}
	adapter, err := NewAdapter(cfg, false)
	if err != nil {
		t.Fatalf("NewAdapter returned unexpected error: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := adapter.Apply(req); err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	if h := req.Header.Get("Authorization"); h != "Bearer tok123" {
		t.Errorf("expected 'Bearer tok123', got %q", h)
	}
}

func TestNewAdapter_UnknownType_ReturnsError(t *testing.T) {
	cfg := config.AuthConfig{Type: "super_secret_method"}
	_, err := NewAdapter(cfg, false)
	if err == nil {
		t.Fatal("expected error for unknown auth type")
	}
}

func TestNewAdapter_PreflightBearer_Created(t *testing.T) {
	cfg := config.AuthConfig{
		Type:     "preflight_bearer",
		URL:      "http://auth.example.com/token",
		Username: "user",
		Password: "pass",
	}
	adapter, err := NewAdapter(cfg, false)
	if err != nil {
		t.Fatalf("NewAdapter returned unexpected error: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}
