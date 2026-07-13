// Package config defines the configuration structures for doris-rest-loader,
// and provides loading, default-injection, and validation logic.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to enable YAML unmarshalling from human-readable
// strings such as "1s", "500ms", "2m".
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a duration string from a YAML scalar node.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = parsed
	return nil
}

// MarshalYAML serialises the duration back to its string representation.
func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

// ─── Root ────────────────────────────────────────────────────────────────────

// Config is the top-level configuration for a single doris-rest-loader instance.
type Config struct {
	Source     SourceConfig     `yaml:"source"`
	Auth       AuthConfig       `yaml:"auth"`
	Pagination PaginationConfig `yaml:"pagination"`
	Threading  ThreadingConfig  `yaml:"threading"`
	Flattening FlatteningConfig `yaml:"flattening"`
	Doris      DorisConfig      `yaml:"doris"`
}

// ─── Source ──────────────────────────────────────────────────────────────────

// SourceConfig defines the RESTful endpoint to fetch data from.
type SourceConfig struct {
	// URL is the endpoint URL. Use {placeholder} syntax for path-injected
	// pagination parameters (e.g. "https://api.example.com/v1/items/{page}/{limit}").
	URL string `yaml:"url"`
	// Method is the HTTP method used for data requests (default: GET).
	Method string `yaml:"method"`
	// Headers contains static HTTP headers applied to every request.
	Headers map[string]string `yaml:"headers"`
	// QueryParams contains static query parameters merged into every request URL.
	QueryParams map[string]string `yaml:"query_params"`
	// DataPath is a dot-notation JSON path pointing to the entity array in the
	// response body (e.g. "data.items"). Defaults to the response root.
	DataPath string `yaml:"data_path"`
	// TLSSkipVerify disables TLS certificate verification. Use only in development.
	TLSSkipVerify bool `yaml:"tls_skip_verify"`
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

// AuthConfig defines authentication settings for the source endpoint.
type AuthConfig struct {
	// Type selects the authentication strategy.
	// Supported: noauth (default), basic, bearer, preflight_bearer, oauth2.
	Type string `yaml:"type"`
	// URL is the authentication endpoint URL used by preflight_bearer.
	URL string `yaml:"url"`
	// Username is used by basic, preflight_bearer, and oauth2 (password grant).
	Username string `yaml:"username"`
	// Password is used by basic, preflight_bearer, and oauth2 (password grant).
	Password string `yaml:"password"`
	// Token is the static Bearer token used by the bearer type.
	Token string `yaml:"token"`
	// GrantType selects the OAuth2 grant type (client_credentials or password).
	// Defaults to client_credentials when type is oauth2.
	GrantType string `yaml:"grant_type"`
	// ClientID is the OAuth2 client identifier.
	ClientID string `yaml:"client_id"`
	// ClientSecret is the OAuth2 client secret.
	ClientSecret string `yaml:"client_secret"`
	// TokenURL is the OAuth2 token endpoint.
	TokenURL string `yaml:"token_url"`
	// Scopes is the list of OAuth2 scopes to request.
	Scopes []string `yaml:"scopes"`
}

// ─── Pagination ───────────────────────────────────────────────────────────────

// PaginationConfig defines how the tool navigates through paginated responses.
type PaginationConfig struct {
	// Type selects the pagination strategy.
	// Supported: page_number (default), offset, cursor (stub).
	Type string `yaml:"type"`
	// Injection defines how pagination parameters are passed to the API.
	// Supported: path (default, implemented), query_params (implemented), body (stub).
	Injection string `yaml:"injection"`
	// StartPage is the first page number (default: 1).
	StartPage int `yaml:"start_page"`
	// PageParam is the URL placeholder name for the page number (default: "page").
	PageParam string `yaml:"page_param"`
	// LimitParam is the URL placeholder name for the page size (default: "limit").
	LimitParam string `yaml:"limit_param"`
	// PageSize is the number of entities per page (default: 100).
	PageSize int `yaml:"page_size"`
	// OffsetParam is the URL placeholder name for the offset value (default: "offset").
	OffsetParam string `yaml:"offset_param"`
	// CursorParam is the URL placeholder or query-param name for the cursor value (default: "cursor").
	CursorParam string `yaml:"cursor_param"`
	// CurrentPagePath is the dot-notation path to the current page number in the response.
	CurrentPagePath string `yaml:"current_page_path"`
	// NumPagesPath is the dot-notation path to the total number of pages in the response.
	NumPagesPath string `yaml:"num_pages_path"`
	// TotalEntriesPath is the dot-notation path to the total entry count in the response.
	TotalEntriesPath string `yaml:"total_entries_path"`
	// CursorPath is the dot-notation path to the next cursor value in the response (cursor stub).
	CursorPath string `yaml:"cursor_path"`
}

// ─── Threading ────────────────────────────────────────────────────────────────

// RetryConfig defines retry and exponential-backoff behaviour for page fetches.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts before giving up (default: 3).
	MaxAttempts int `yaml:"max_attempts"`
	// InitialDelay is the wait time before the first retry (default: 1s).
	InitialDelay Duration `yaml:"initial_delay"`
	// Multiplier is applied to the delay after each failed attempt (default: 2.0).
	Multiplier float64 `yaml:"multiplier"`
	// MaxDelay caps the backoff delay (default: 30s).
	MaxDelay Duration `yaml:"max_delay"`
}

// ThreadingConfig defines concurrency settings for parallel page fetching.
type ThreadingConfig struct {
	// MaxGoroutines is the maximum number of concurrent page-fetching goroutines (default: 5).
	MaxGoroutines int `yaml:"max_goroutines"`
	// Retry configures retry and backoff behaviour for each page fetch.
	Retry RetryConfig `yaml:"retry"`
}

// ─── Flattening ───────────────────────────────────────────────────────────────

// FieldMapping selects a single field from an entity and optionally renames it.
type FieldMapping struct {
	// Path is the dot-notation JSON path to the field within an entity.
	Path string `yaml:"path"`
	// As is the output column name. Defaults to Path when empty.
	As string `yaml:"as"`
}

// FlatteningConfig defines how raw entities are transformed before ingestion.
type FlatteningConfig struct {
	// Enabled controls recursive dot-notation flattening of nested objects.
	// Defaults to true when not specified. Set explicitly to false to disable.
	Enabled *bool `yaml:"enabled"`
	// Separator is the character used to join nested key names (default: ".").
	Separator string `yaml:"separator"`
	// MaxDepth limits how many nesting levels are unrolled into dot-notation
	// columns. A value of 0 (default) means unlimited depth — every nested
	// object is flattened recursively until a scalar or array leaf is reached.
	// When set to a positive value N, objects at depth N are kept as-is
	// (stored as a Doris JSON column rather than being further flattened).
	// For example, with MaxDepth: 1 the entity
	//   {"project": {"id": 1, "meta": {"color": "red"}}}
	// produces columns "project.id" (DOUBLE) and "project.meta" (JSON)
	// instead of three columns including "project.meta.color".
	MaxDepth int `yaml:"max_depth"`
	// Include narrows the output to only the listed fields (with optional rename).
	// When set, only these fields are emitted; flattening is still applied to
	// nested values within each selected field if Enabled is true.
	Include []FieldMapping `yaml:"include"`
}

// ─── Doris ────────────────────────────────────────────────────────────────────

// DorisConfig defines the Apache Doris Stream Load target.
type DorisConfig struct {
	// Host is the Doris FE HTTP address (e.g. "http://doris-fe:8030").
	Host string `yaml:"host"`
	// Database is the target Doris database (fixed per config file).
	Database string `yaml:"database"`
	// Table is the target Doris table (configurable per endpoint).
	Table string `yaml:"table"`
	// User is the Doris user for Stream Load Basic Auth (default: "root").
	User string `yaml:"user"`
	// Password is the Doris user password.
	Password string `yaml:"password"`
	// TLSSkipVerify disables TLS certificate verification. Use only in development.
	TLSSkipVerify bool `yaml:"tls_skip_verify"`
}

// ─── Loader ───────────────────────────────────────────────────────────────────

// Load reads the YAML file at path, applies defaults, and validates the result.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse YAML: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return &cfg, nil
}

// ─── Defaults ─────────────────────────────────────────────────────────────────

// applyDefaults fills in sensible values for any fields left unset by the caller.
func applyDefaults(cfg *Config) {
	if cfg.Source.Method == "" {
		cfg.Source.Method = "GET"
	}

	if cfg.Auth.Type == "" {
		cfg.Auth.Type = "noauth"
	}

	if cfg.Pagination.Type == "" {
		cfg.Pagination.Type = "page_number"
	}
	if cfg.Pagination.Injection == "" {
		cfg.Pagination.Injection = "path"
	}
	if cfg.Pagination.StartPage == 0 {
		cfg.Pagination.StartPage = 1
	}
	if cfg.Pagination.PageParam == "" {
		cfg.Pagination.PageParam = "page"
	}
	if cfg.Pagination.LimitParam == "" {
		cfg.Pagination.LimitParam = "limit"
	}
	if cfg.Pagination.OffsetParam == "" {
		cfg.Pagination.OffsetParam = "offset"
	}
	if cfg.Pagination.CursorParam == "" {
		cfg.Pagination.CursorParam = "cursor"
	}
	if cfg.Pagination.PageSize == 0 {
		cfg.Pagination.PageSize = 100
	}

	if cfg.Threading.MaxGoroutines == 0 {
		cfg.Threading.MaxGoroutines = 5
	}
	if cfg.Threading.Retry.MaxAttempts == 0 {
		cfg.Threading.Retry.MaxAttempts = 3
	}
	if cfg.Threading.Retry.InitialDelay.Duration == 0 {
		cfg.Threading.Retry.InitialDelay.Duration = time.Second
	}
	if cfg.Threading.Retry.Multiplier == 0 {
		cfg.Threading.Retry.Multiplier = 2.0
	}
	if cfg.Threading.Retry.MaxDelay.Duration == 0 {
		cfg.Threading.Retry.MaxDelay.Duration = 30 * time.Second
	}

	// Flattening is enabled by default. Use *bool so an explicit "enabled: false"
	// in YAML can be distinguished from an absent key.
	if cfg.Flattening.Enabled == nil {
		t := true
		cfg.Flattening.Enabled = &t
	}
	if cfg.Flattening.Separator == "" {
		cfg.Flattening.Separator = "."
	}

	if cfg.Doris.User == "" {
		cfg.Doris.User = "root"
	}

	if cfg.Doris.Database == "" {
		cfg.Doris.User = "bronze"
	}
}

// ─── Validation ───────────────────────────────────────────────────────────────

// validate checks that the configuration is self-consistent and complete.
func validate(cfg *Config) error {
	if cfg.Source.URL == "" {
		return fmt.Errorf("source.url is required")
	}
	if cfg.Doris.Host == "" {
		return fmt.Errorf("doris.host is required")
	}
	if cfg.Doris.Database == "" {
		return fmt.Errorf("doris.database is required")
	}
	if cfg.Doris.Table == "" {
		return fmt.Errorf("doris.table is required")
	}

	if err := validateAuth(cfg.Auth); err != nil {
		return err
	}
	if err := validatePagination(cfg.Pagination); err != nil {
		return err
	}
	return nil
}

func validateAuth(a AuthConfig) error {
	switch a.Type {
	case "noauth":
		// No additional fields required.
	case "basic":
		if a.Username == "" {
			return fmt.Errorf("auth.username is required for auth type %q", a.Type)
		}
	case "bearer":
		if a.Token == "" {
			return fmt.Errorf("auth.token is required for auth type %q", a.Type)
		}
	case "preflight_bearer":
		if a.URL == "" {
			return fmt.Errorf("auth.url is required for auth type %q", a.Type)
		}
		if a.Username == "" {
			return fmt.Errorf("auth.username is required for auth type %q", a.Type)
		}
	case "oauth2":
		if a.TokenURL == "" {
			return fmt.Errorf("auth.token_url is required for auth type %q", a.Type)
		}
		if a.ClientID == "" {
			return fmt.Errorf("auth.client_id is required for auth type %q", a.Type)
		}
		switch a.GrantType {
		case "", "client_credentials", "password":
			// valid
		default:
			return fmt.Errorf("auth.grant_type %q is not supported (supported: client_credentials, password)", a.GrantType)
		}
	default:
		return fmt.Errorf("auth.type %q is not supported (supported: noauth, basic, bearer, preflight_bearer, oauth2)", a.Type)
	}
	return nil
}

func validatePagination(p PaginationConfig) error {
	switch p.Injection {
	case "path", "query_params", "body":
		// valid
	default:
		return fmt.Errorf("pagination.injection %q is not supported (supported: path, query_params, body)", p.Injection)
	}

	switch p.Type {
	case "page_number":
		hasNumPages := p.NumPagesPath != ""
		hasTotalEntries := p.TotalEntriesPath != "" && p.PageSize > 0
		if !hasNumPages && !hasTotalEntries {
			return fmt.Errorf(
				"pagination type %q requires either 'num_pages_path' or both 'total_entries_path' and 'page_size'",
				p.Type,
			)
		}
	case "offset":
		if p.TotalEntriesPath == "" {
			return fmt.Errorf("pagination type %q requires 'total_entries_path'", p.Type)
		}
		if p.PageSize == 0 {
			return fmt.Errorf("pagination type %q requires 'page_size'", p.Type)
		}
	case "cursor":
		if p.CursorPath == "" {
			return fmt.Errorf("pagination type %q requires 'cursor_path'", p.Type)
		}
	default:
		return fmt.Errorf("pagination.type %q is not supported (supported: page_number, offset, cursor)", p.Type)
	}
	return nil
}
