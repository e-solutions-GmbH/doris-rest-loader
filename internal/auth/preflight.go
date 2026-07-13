package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PreflightBearer performs a one-time POST to an authentication endpoint,
// sending a JSON body of {"username": "…", "password": "…"}, and extracts a
// Bearer token from the response. The token is then attached to all subsequent
// data requests via the Authorization header.
//
// The response JSON is inspected for the following common token field names
// (in order of preference): "token", "access_token", "accessToken".
type PreflightBearer struct {
	authURL  string
	username string
	password string
	client   *http.Client
	token    string
}

// NewPreflightBearer creates a new PreflightBearer adapter.
// The supplied http.Client is used exclusively for the preflight request and
// should be configured with appropriate TLS settings for the auth endpoint.
func NewPreflightBearer(authURL, username, password string, client *http.Client) *PreflightBearer {
	return &PreflightBearer{
		authURL:  authURL,
		username: username,
		password: password,
		client:   client,
	}
}

// Prepare POSTs the credentials to the auth URL and stores the returned token.
// Must be called before Apply.
func (p *PreflightBearer) Prepare(ctx context.Context) error {
	payload, err := json.Marshal(map[string]string{
		"username": p.username,
		"password": p.password,
	})
	if err != nil {
		return fmt.Errorf("auth: marshal preflight credentials: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.authURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("auth: create preflight request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: preflight request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("auth: read preflight response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("auth: preflight request returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("auth: decode preflight response JSON: %w", err)
	}

	token, ok := p.extractToken(result)
	if !ok {
		return fmt.Errorf("auth: no 'token', 'access_token', or 'accessToken' field found in preflight response")
	}

	p.token = token
	return nil
}

// extractToken searches the response map for common token field names.
func (p *PreflightBearer) extractToken(result map[string]any) (string, bool) {
	for _, key := range []string{"token", "access_token", "accessToken"} {
		if raw, ok := result[key]; ok {
			if s, ok := raw.(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

// Apply sets the Authorization header with the obtained Bearer token.
// Returns an error if Prepare has not been called yet.
func (p *PreflightBearer) Apply(req *http.Request) error {
	if p.token == "" {
		return fmt.Errorf("auth: preflight token not yet obtained; call Prepare before Apply")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	return nil
}
