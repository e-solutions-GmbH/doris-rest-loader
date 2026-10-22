// Package config defines the configuration structures for doris-rest-loader,
// and provides loading, default-injection, and validation logic.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults applied by applyDefaults when the corresponding YAML fields are
// unset. Exported so callers and tests can reference them symbolically instead
// of duplicating string literals.
const (
	// DefaultDorisUser is the Doris factory admin account, matching the
	// MySQL/Postgres convention. Override via `doris.user` in YAML.
	DefaultDorisUser = "root"
	// DefaultDorisDatabase is dorest's conventional ingest target — the
	// medallion Bronze/staging layer. Override via `doris.database` in YAML.
	DefaultDorisDatabase = "bronze"
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
	// When FanOut is configured, URL may additionally contain the fan-out
	// placeholder (e.g. "{item}") substituted once per fan-out item.
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
	// FanOut, when set, switches the runner into the two-stage "list, then fan
	// out a parameterised call per list entry" pagination mode. Absent (nil)
	// is the default and is fully backward compatible with the original
	// single-stage paginated fetch.
	FanOut *FanOutConfig `yaml:"fanout"`
}

// ─── Fan-out ────────────────────────────────────────────────────────────────

// FanOutConfig defines the "list, then fan out a parameterised call per list
// entry" source mode: a list endpoint enumerates parent entities/identifiers,
// and one detail request (source.url, with the fan-out item's fields
// substituted in) is issued per identifier.
//
// BREAKING CHANGE: the former ItemField/ItemPlaceholder/ItemColumn keys have
// been unified into the single polymorphic ItemFields key. There is no
// backward-compatible alias for the old keys.
type FanOutConfig struct {
	// ListURL is the endpoint that enumerates the parent entities (required).
	ListURL string `yaml:"list_url"`
	// ListDataPath is a dot-notation path to the array of parent entities in
	// the list response. Defaults to the response root.
	ListDataPath string `yaml:"list_data_path"`
	// ItemFields selects one or more dot-notation fields (within one list
	// entity) to fan out on (required, non-empty). Each selected field is
	// both a named URL placeholder substituted into source.url (and, when
	// SubList is configured, into sub_list.list_url) and an output column
	// auto-injected into every entity extracted from that item's detail
	// response(s) — there is no separate item_column key. See FieldSpec for
	// the supported YAML shapes.
	ItemFields FieldSpec `yaml:"item_fields"`
	// ListPagination optionally paginates the list fetch itself, using the
	// same schema/semantics as the top-level `pagination` block. Omit if the
	// list endpoint returns everything in a single response.
	ListPagination PaginationConfig `yaml:"list_pagination"`
	// SubList, when set, fetches a per-item paginated sub-list (e.g. a
	// commit history) before issuing the item's detail request, extracting
	// additional field values from the first entity of the first page and
	// the last entity of the last page, and merging those into the item's
	// values (available as URL placeholders and auto-injected columns,
	// exactly like ItemFields). Optional; unset (default) means no sub-list
	// stage, fully backward compatible with the plain list/fan-out mode.
	SubList *SubListConfig `yaml:"sub_list"`
}

// SubListConfig defines a per-fan-out-item nested paginated sub-list fetch,
// used to derive additional item values (e.g. the first and last commit of a
// repository's history) before the item's main detail request is issued.
//
// The sub-list fetch reuses the same fetcher/auth/retry configuration as the
// rest of the run; it is not a separate auth context. Walking the sub-list's
// pages happens sequentially within the fan-out item's own goroutine —
// concurrency is only ever applied across fan-out items
// (threading.max_goroutines), never across a single item's sub-list pages.
//
// An empty sub-list result (zero entities on the first/last page) is treated
// as a hard error for that fan-out item, propagated through the same
// per-item failure/aggregation mechanism as any other item-level failure —
// there is no silent empty-placeholder fallback.
type SubListConfig struct {
	// ListURL is the per-item sub-list endpoint (required). It may reference
	// any of the outer item's ItemFields placeholders (e.g.
	// "https://api.example.com/projects/{key}/repos/{slug}/commits"); those
	// are substituted before the sub-list fetch runs.
	ListURL string `yaml:"list_url"`
	// ListDataPath is a dot-notation path to the array of entities in the
	// sub-list response. Defaults to the response root.
	ListDataPath string `yaml:"list_data_path"`
	// Pagination optionally paginates the sub-list fetch, using the same
	// schema/semantics as the top-level `pagination` block. Omit if the
	// sub-list endpoint returns everything in a single response.
	Pagination PaginationConfig `yaml:"pagination"`
	// FirstItemFields selects one or more dot-notation fields, extracted
	// from the first entity of the sub-list's first page (required,
	// non-empty). Uses the same FieldSpec shapes as ItemFields, and each
	// selected field is likewise auto-injected as an output column.
	FirstItemFields FieldSpec `yaml:"first_item_fields"`
	// LastItemFields selects one or more dot-notation fields, extracted
	// from the last entity of the sub-list's last page (required,
	// non-empty). Uses the same FieldSpec shapes as ItemFields, and each
	// selected field is likewise auto-injected as an output column.
	LastItemFields FieldSpec `yaml:"last_item_fields"`
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
	// Supported: page_number (default), offset, cursor.
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
	// MaxPages caps the number of pages fetched (default: 0 = unlimited).
	MaxPages int `yaml:"max_pages"`
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
	// CursorPath is the dot-notation path to the next cursor value in the response.
	CursorPath string `yaml:"cursor_path"`
	// LastPagePath is an optional dot-notation path to a boolean "is last
	// page" signal in the response (e.g. Bitbucket's "isLastPage"), used by
	// the cursor adapter to terminate pagination. When empty, the cursor
	// adapter instead terminates as soon as no cursor value is present in
	// the response (i.e. CursorPath resolves to nothing / an empty value).
	LastPagePath string `yaml:"last_page_path"`
}

// IsZero reports whether p is the zero-value PaginationConfig, i.e. nothing
// was configured. Used by source.fanout.list_pagination to distinguish "not
// configured" (the list endpoint returns everything in one response) from an
// explicitly configured block. All fields are comparable, so direct equality
// is sufficient.
func (p PaginationConfig) IsZero() bool {
	return p == PaginationConfig{}
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
	// RawJSON, when non-empty, adds a column with this name containing the full
	// original entity serialised as a JSON string before any flattening or field
	// selection is applied. Use this to preserve the complete raw payload in
	// bronze for future reprocessing without re-fetching.
	// Example: raw_json_column: "raw_json"
	RawJSON string `yaml:"raw_json_column"`
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

	applyPaginationDefaults(&cfg.Pagination)

	// fan_out is purely additive: only touch it (and its optional nested
	// list_pagination) when the block is actually present in the config.
	if cfg.Source.FanOut != nil {
		// list_pagination itself is optional ("omit if the list endpoint
		// returns everything in a single response"); only apply defaults
		// when the user configured at least one of its fields. Otherwise
		// leave it as the zero value so validate()/the runner treat it as
		// absent rather than requiring e.g. total_entries_path.
		if !cfg.Source.FanOut.ListPagination.IsZero() {
			applyPaginationDefaults(&cfg.Source.FanOut.ListPagination)
		}
		// sub_list is likewise optional, and its own pagination block
		// follows the same "only apply defaults if actually configured"
		// rule as list_pagination above.
		if sl := cfg.Source.FanOut.SubList; sl != nil {
			if !sl.Pagination.IsZero() {
				applyPaginationDefaults(&sl.Pagination)
			}
		}
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
		cfg.Doris.User = DefaultDorisUser
	}
	if cfg.Doris.Database == "" {
		cfg.Doris.Database = DefaultDorisDatabase
	}
}

// applyPaginationDefaults fills in sensible values for an unset
// PaginationConfig. Shared between the top-level `pagination` block (always
// applied) and the optional `source.fanout.list_pagination` block (applied
// only when the latter was actually configured).
func applyPaginationDefaults(p *PaginationConfig) {
	if p.Type == "" {
		p.Type = "page_number"
	}
	if p.Injection == "" {
		p.Injection = "path"
	}
	if p.StartPage == 0 {
		p.StartPage = 1
	}
	if p.PageParam == "" {
		p.PageParam = "page"
	}
	if p.LimitParam == "" {
		p.LimitParam = "limit"
	}
	if p.OffsetParam == "" {
		p.OffsetParam = "offset"
	}
	if p.CursorParam == "" {
		p.CursorParam = "cursor"
	}
	if p.PageSize == 0 {
		p.PageSize = 100
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
	if err := validateFanOut(cfg.Source); err != nil {
		return err
	}
	return nil
}

// validateFanOut checks the optional source.fanout block. Absent (nil) is
// always valid — fan_out is purely additive.
func validateFanOut(s SourceConfig) error {
	fo := s.FanOut
	if fo == nil {
		return nil
	}
	if fo.ListURL == "" {
		return fmt.Errorf("source.fanout.list_url is required")
	}
	if fo.ItemFields.IsEmpty() {
		return fmt.Errorf("source.fanout.item_fields is required")
	}

	seen := make(map[string]string) // placeholder -> originating field spec, for duplicate detection
	if err := requirePlaceholdersInURL(s.URL, fo.ItemFields, "source.fanout.item_fields", seen); err != nil {
		return err
	}

	if !fo.ListPagination.IsZero() {
		if err := validatePagination(fo.ListPagination); err != nil {
			return fmt.Errorf("source.fanout.list_pagination: %w", err)
		}
	}

	if sl := fo.SubList; sl != nil {
		if sl.ListURL == "" {
			return fmt.Errorf("source.fanout.sub_list.list_url is required")
		}
		if sl.FirstItemFields.IsEmpty() {
			return fmt.Errorf("source.fanout.sub_list.first_item_fields is required")
		}
		if sl.LastItemFields.IsEmpty() {
			return fmt.Errorf("source.fanout.sub_list.last_item_fields is required")
		}
		if err := requirePlaceholdersInURL(s.URL, sl.FirstItemFields, "source.fanout.sub_list.first_item_fields", seen); err != nil {
			return err
		}
		if err := requirePlaceholdersInURL(s.URL, sl.LastItemFields, "source.fanout.sub_list.last_item_fields", seen); err != nil {
			return err
		}
		if !sl.Pagination.IsZero() {
			if err := validatePagination(sl.Pagination); err != nil {
				return fmt.Errorf("source.fanout.sub_list.pagination: %w", err)
			}
		}
	}

	return nil
}

// requirePlaceholdersInURL verifies that every placeholder named by spec
// appears as "{placeholder}" in url, and records each placeholder name in
// seen (keyed by originating field-spec label) to catch duplicate
// placeholder names across item_fields/first_item_fields/last_item_fields —
// all of which merge into one values map per fan-out item, so a collision
// would silently let one field's value overwrite another's.
func requirePlaceholdersInURL(url string, spec FieldSpec, label string, seen map[string]string) error {
	for _, entry := range spec {
		if prevLabel, ok := seen[entry.Placeholder]; ok && prevLabel != label {
			return fmt.Errorf(
				"placeholder %q is used by both %s and %s; placeholder names must be unique across item_fields, sub_list.first_item_fields, and sub_list.last_item_fields",
				entry.Placeholder, prevLabel, label)
		}
		seen[entry.Placeholder] = label

		placeholder := "{" + entry.Placeholder + "}"
		if !strings.Contains(url, placeholder) {
			return fmt.Errorf(
				"source.url must contain the fan-out placeholder %q (from %s)", placeholder, label)
		}
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
