// Package fetcher provides an HTTP client that fetches JSON from REST endpoints
// with configurable authentication, static headers/query-params, and retry logic.
package fetcher

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/auth"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// Fetcher executes authenticated HTTP GET (or other) requests against a REST
// endpoint and returns the decoded JSON response.
// Retry with exponential backoff is applied automatically on each call.
type Fetcher struct {
	client      *http.Client
	sourceCfg   config.SourceConfig
	retryCfg    config.RetryConfig
	authAdapter auth.Adapter
}

// New creates a new Fetcher configured for the given source and retry settings.
func New(sourceCfg config.SourceConfig, retryCfg config.RetryConfig, authAdapter auth.Adapter) *Fetcher {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: sourceCfg.TLSSkipVerify}, //nolint:gosec
	}
	return &Fetcher{
		client:      &http.Client{Transport: transport},
		sourceCfg:   sourceCfg,
		retryCfg:    retryCfg,
		authAdapter: authAdapter,
	}
}

// FetchWithRetry fetches targetURL, retrying on transient errors according to
// the configured retry policy. The JSON response body is decoded and returned as
// an any value (either map[string]any for a JSON object, or []any for a JSON array).
func (f *Fetcher) FetchWithRetry(ctx context.Context, targetURL string) (any, error) {
	var result any
	err := WithRetry(ctx, f.retryCfg, func() error {
		var fetchErr error
		result, fetchErr = f.fetch(ctx, targetURL)
		return fetchErr
	})
	if err != nil {
		return nil, fmt.Errorf("fetcher: %s %s: %w", f.sourceCfg.Method, targetURL, err)
	}
	return result, nil
}

// fetch performs a single HTTP request and decodes the JSON response.
func (f *Fetcher) fetch(ctx context.Context, targetURL string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, f.sourceCfg.Method, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Apply static headers from config.
	for k, v := range f.sourceCfg.Headers {
		req.Header.Set(k, v)
	}
	// Default Accept header when none is explicitly configured.
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// Merge static query parameters from config into the URL.
	if len(f.sourceCfg.QueryParams) > 0 {
		q := req.URL.Query()
		for k, v := range f.sourceCfg.QueryParams {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Apply authentication (sets headers, basic auth, etc.).
	if err := f.authAdapter.Apply(req); err != nil {
		return nil, fmt.Errorf("apply auth: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected HTTP status %d: %s", resp.StatusCode, string(body))
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode JSON response: %w", err)
	}

	return decoded, nil
}
