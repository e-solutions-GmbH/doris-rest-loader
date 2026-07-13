// Package config — environment variable overrides.
//
// After a config file is loaded with Load(), call ApplyEnvOverrides to merge
// any values supplied via environment variables. Env vars take precedence over
// the YAML file, which makes Kubernetes CronJob deployments flexible: stable
// defaults live in a ConfigMap-mounted YAML while dynamic, per-run values (e.g.
// the calendar week to ingest) are injected as env vars in the Job spec.
//
// Supported variables:
//
//	SOURCE_QUERY_PARAMS   JSON object — merged into source.query_params.
//	                      Existing keys are overridden; absent keys are kept.
//	                      Example: {"filter[calendarweek]":"202526,202527"}
//
//	DRY_RUN               Handled in main; listed here for completeness.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ApplyEnvOverrides merges environment variable overrides into cfg after the
// YAML file has been loaded. It must be called before runner.New so that the
// final merged config reaches all adapters.
func ApplyEnvOverrides(cfg *Config) error {
	if err := applySourceQueryParams(cfg); err != nil {
		return err
	}
	return nil
}

// applySourceQueryParams reads SOURCE_QUERY_PARAMS and merges its entries into
// cfg.Source.QueryParams. The env var must be a JSON object whose keys and
// values are both strings. Keys present in the env var override the same keys
// from the YAML; keys absent from the env var are left unchanged.
func applySourceQueryParams(cfg *Config) error {
	raw := os.Getenv("SOURCE_QUERY_PARAMS")
	if raw == "" {
		return nil
	}

	var overrides map[string]string
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		return fmt.Errorf("env SOURCE_QUERY_PARAMS: invalid JSON: %w", err)
	}

	if cfg.Source.QueryParams == nil {
		cfg.Source.QueryParams = make(map[string]string)
	}
	for k, v := range overrides {
		cfg.Source.QueryParams[k] = v
	}
	return nil
}
