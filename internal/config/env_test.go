package config

import (
	"os"
	"testing"
)

func TestApplyEnvOverrides_AddsNewQueryParam(t *testing.T) {
	cfg := &Config{
		Source: SourceConfig{
			QueryParams: map[string]string{"sort": "build_date"},
		},
	}
	t.Setenv("SOURCE_QUERY_PARAMS", `{"filter[calendarweek]":"202526,202527"}`)

	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Source.QueryParams["filter[calendarweek]"] != "202526,202527" {
		t.Errorf("expected calendarweek to be set, got %q", cfg.Source.QueryParams["filter[calendarweek]"])
	}
	// Existing key must be preserved.
	if cfg.Source.QueryParams["sort"] != "build_date" {
		t.Errorf("existing key 'sort' must not be removed, got %q", cfg.Source.QueryParams["sort"])
	}
}

func TestApplyEnvOverrides_OverridesExistingQueryParam(t *testing.T) {
	cfg := &Config{
		Source: SourceConfig{
			QueryParams: map[string]string{"filter[calendarweek]": "202501"},
		},
	}
	t.Setenv("SOURCE_QUERY_PARAMS", `{"filter[calendarweek]":"202526"}`)

	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Source.QueryParams["filter[calendarweek]"] != "202526" {
		t.Errorf("expected override to win, got %q", cfg.Source.QueryParams["filter[calendarweek]"])
	}
}

func TestApplyEnvOverrides_NilQueryParamsInitialised(t *testing.T) {
	cfg := &Config{Source: SourceConfig{QueryParams: nil}}
	t.Setenv("SOURCE_QUERY_PARAMS", `{"sort":"name"}`)

	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Source.QueryParams["sort"] != "name" {
		t.Errorf("expected sort=name, got %q", cfg.Source.QueryParams["sort"])
	}
}

func TestApplyEnvOverrides_EnvVarNotSet_NoOp(t *testing.T) {
	os.Unsetenv("SOURCE_QUERY_PARAMS")
	cfg := &Config{
		Source: SourceConfig{
			QueryParams: map[string]string{"sort": "id"},
		},
	}
	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Source.QueryParams) != 1 || cfg.Source.QueryParams["sort"] != "id" {
		t.Errorf("config must be unchanged when env var is absent: %v", cfg.Source.QueryParams)
	}
}

func TestApplyEnvOverrides_InvalidJSON_ReturnsError(t *testing.T) {
	cfg := &Config{}
	t.Setenv("SOURCE_QUERY_PARAMS", `not-json`)

	if err := ApplyEnvOverrides(cfg); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestApplyEnvOverrides_MultipleParams(t *testing.T) {
	cfg := &Config{Source: SourceConfig{QueryParams: map[string]string{}}}
	t.Setenv("SOURCE_QUERY_PARAMS", `{
		"filter[calendarweek]": "202526,202527",
		"filter[projects]":     "1,2",
		"sort":                 "build_date"
	}`)

	if err := ApplyEnvOverrides(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"filter[calendarweek]": "202526,202527",
		"filter[projects]":     "1,2",
		"sort":                 "build_date",
	}
	for k, v := range want {
		if cfg.Source.QueryParams[k] != v {
			t.Errorf("param %q: expected %q, got %q", k, v, cfg.Source.QueryParams[k])
		}
	}
}
