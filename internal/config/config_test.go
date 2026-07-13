package config

import (
	"os"
	"testing"
	"time"
)

// boolPtr is a test helper to create a *bool literal.
func boolPtr(b bool) *bool { return &b }

// writeTempConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

func TestLoad_MinimalValid(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/v1/items/{page}/{limit}"
pagination:
  num_pages_path: "meta.totalPages"
doris:
  host: "http://doris-fe:8030"
  database: "mydb"
  table: "events"
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Source defaults
	if cfg.Source.Method != "GET" {
		t.Errorf("expected Method=GET, got %q", cfg.Source.Method)
	}

	// Auth default
	if cfg.Auth.Type != "noauth" {
		t.Errorf("expected auth type=noauth, got %q", cfg.Auth.Type)
	}

	// Pagination defaults
	if cfg.Pagination.Type != "page_number" {
		t.Errorf("expected pagination type=page_number, got %q", cfg.Pagination.Type)
	}
	if cfg.Pagination.StartPage != 1 {
		t.Errorf("expected StartPage=1, got %d", cfg.Pagination.StartPage)
	}
	if cfg.Pagination.PageSize != 100 {
		t.Errorf("expected PageSize=100, got %d", cfg.Pagination.PageSize)
	}
	if cfg.Pagination.PageParam != "page" {
		t.Errorf("expected PageParam=page, got %q", cfg.Pagination.PageParam)
	}
	if cfg.Pagination.LimitParam != "limit" {
		t.Errorf("expected LimitParam=limit, got %q", cfg.Pagination.LimitParam)
	}

	// Threading defaults
	if cfg.Threading.MaxGoroutines != 5 {
		t.Errorf("expected MaxGoroutines=5, got %d", cfg.Threading.MaxGoroutines)
	}
	if cfg.Threading.Retry.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.Threading.Retry.MaxAttempts)
	}
	if cfg.Threading.Retry.InitialDelay.Duration != time.Second {
		t.Errorf("expected InitialDelay=1s, got %v", cfg.Threading.Retry.InitialDelay.Duration)
	}
	if cfg.Threading.Retry.Multiplier != 2.0 {
		t.Errorf("expected Multiplier=2.0, got %f", cfg.Threading.Retry.Multiplier)
	}
	if cfg.Threading.Retry.MaxDelay.Duration != 30*time.Second {
		t.Errorf("expected MaxDelay=30s, got %v", cfg.Threading.Retry.MaxDelay.Duration)
	}

	// Flattening defaults
	if cfg.Flattening.Enabled == nil || !*cfg.Flattening.Enabled {
		t.Error("expected Flattening.Enabled=true by default")
	}
	if cfg.Flattening.Separator != "." {
		t.Errorf("expected Separator='.', got %q", cfg.Flattening.Separator)
	}

	// Doris user default
	if cfg.Doris.User != "root" {
		t.Errorf("expected Doris.User=root, got %q", cfg.Doris.User)
	}
}

func TestLoad_FlatteningDisabledExplicitly(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  num_pages_path: "pages"
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
flattening:
  enabled: false
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Flattening.Enabled == nil || *cfg.Flattening.Enabled {
		t.Error("expected Flattening.Enabled=false after explicit config")
	}
}

func TestLoad_CustomDurations(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items/{page}/{limit}"
pagination:
  num_pages_path: "meta.pages"
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
threading:
  retry:
    initial_delay: "500ms"
    max_delay: "10s"
    multiplier: 1.5
    max_attempts: 5
`
	cfg, err := Load(writeTempConfig(t, yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Threading.Retry.InitialDelay.Duration != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %v", cfg.Threading.Retry.InitialDelay)
	}
	if cfg.Threading.Retry.MaxDelay.Duration != 10*time.Second {
		t.Errorf("expected 10s, got %v", cfg.Threading.Retry.MaxDelay)
	}
	if cfg.Threading.Retry.Multiplier != 1.5 {
		t.Errorf("expected 1.5, got %f", cfg.Threading.Retry.Multiplier)
	}
	if cfg.Threading.Retry.MaxAttempts != 5 {
		t.Errorf("expected 5, got %d", cfg.Threading.Retry.MaxAttempts)
	}
}

func TestLoad_MissingSourceURL(t *testing.T) {
	yaml := `
pagination:
  num_pages_path: "pages"
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing source.url")
	}
}

func TestLoad_MissingDorisHost(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  num_pages_path: "pages"
doris:
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for missing doris.host")
	}
}

func TestLoad_PaginationConsistency_PageNumberWithoutPaths(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  type: page_number
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for page_number without num_pages_path or total_entries_path")
	}
}

func TestLoad_PaginationConsistency_OffsetWithoutTotalEntries(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  type: offset
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for offset without total_entries_path")
	}
}

func TestLoad_AuthBearer_MissingToken(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  num_pages_path: "pages"
auth:
  type: bearer
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for bearer auth without token")
	}
}

func TestLoad_UnknownAuthType(t *testing.T) {
	yaml := `
source:
  url: "https://api.example.com/items"
pagination:
  num_pages_path: "pages"
auth:
  type: magic_auth
doris:
  host: "http://doris:8030"
  database: "db"
  table: "t"
`
	_, err := Load(writeTempConfig(t, yaml))
	if err == nil {
		t.Fatal("expected error for unknown auth type")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/tmp/this-file-does-not-exist-doris-rest-loader.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
