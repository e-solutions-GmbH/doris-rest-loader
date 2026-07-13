// Package auth provides authentication adapters for the doris-rest-loader.
// Each adapter implements the Adapter interface, which decouples authentication
// strategy from the HTTP fetching layer.
package auth

import (
	"context"
	"fmt"
	"net/http"
)

// Adapter is the interface that all authentication strategies must implement.
type Adapter interface {
	// Prepare performs any pre-flight steps required before data fetching begins,
	// such as fetching an initial token. It is called exactly once before the
	// first request. Implementations that require no preparation should return nil.
	Prepare(ctx context.Context) error

	// Apply attaches the authentication credentials to the outgoing request.
	// It is called for every individual HTTP request made by the fetcher.
	Apply(req *http.Request) error
}

// ─── NoAuth ───────────────────────────────────────────────────────────────────

// NoAuth is an Adapter that performs no authentication.
// Use it when the target endpoint is publicly accessible.
type NoAuth struct{}

// Prepare is a no-op for NoAuth.
func (n *NoAuth) Prepare(_ context.Context) error { return nil }

// Apply is a no-op for NoAuth.
func (n *NoAuth) Apply(_ *http.Request) error { return nil }

// ─── BasicAuth ────────────────────────────────────────────────────────────────

// BasicAuth applies HTTP Basic Authentication (RFC 7617) to every request.
type BasicAuth struct {
	Username string
	Password string
}

// Prepare is a no-op for BasicAuth.
func (b *BasicAuth) Prepare(_ context.Context) error { return nil }

// Apply sets the Authorization header using Base64-encoded username:password.
func (b *BasicAuth) Apply(req *http.Request) error {
	req.SetBasicAuth(b.Username, b.Password)
	return nil
}

// ─── BearerAuth ───────────────────────────────────────────────────────────────

// BearerAuth applies a static Bearer token to every request.
// Use this when the token is long-lived and provided directly in the config.
type BearerAuth struct {
	Token string
}

// Prepare is a no-op for BearerAuth.
func (b *BearerAuth) Prepare(_ context.Context) error { return nil }

// Apply sets the Authorization header with the configured Bearer token.
func (b *BearerAuth) Apply(req *http.Request) error {
	if b.Token == "" {
		return fmt.Errorf("auth: bearer token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+b.Token)
	return nil
}
