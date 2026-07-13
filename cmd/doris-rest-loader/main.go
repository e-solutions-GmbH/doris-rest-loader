// Command doris-rest-loader ingests data from a RESTful endpoint into Apache Doris.
//
// Usage:
//
//	doris-rest-loader --config /path/to/config.yaml
//	doris-rest-loader --config /path/to/config.yaml --dry-run
//
// The --dry-run flag fetches the first page of data, runs it through the full
// flattening pipeline, and prints an inferred CREATE TABLE DDL to stdout.
// Nothing is written to Doris. Use this to inspect the column structure and
// create the target table before running a real ingestion.
//
// In Docker / Kubernetes the same mode can be activated via the DRY_RUN
// environment variable (DRY_RUN=1 or DRY_RUN=true) without needing to
// reconstruct the CMD arguments:
//
//	docker run --rm -v ./config.yaml:/etc/doris-rest-loader/config.yaml \
//	  -e DRY_RUN=1 ghcr.io/e-solutions-gmbh/doris-rest-loader
//
// Environment variable overrides (applied after the config file is loaded):
//
//	SOURCE_QUERY_PARAMS   JSON object merged into source.query_params.
//	                      Keys in this variable override same-named keys from
//	                      the YAML; absent keys are left unchanged. Use this in
//	                      Kubernetes CronJobs to inject dynamic values (e.g. the
//	                      current calendar week) without rebuilding the image or
//	                      editing the ConfigMap.
//	                      Example: {"filter[calendarweek]":"202526,202527"}
//
// The configuration file is a YAML document that specifies the source endpoint,
// authentication, pagination strategy, data flattening, and Doris target.
// See the project README for a full configuration reference.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/runner"
)

func main() {
	configPath := flag.String(
		"config",
		"/etc/doris-rest-loader/config.yaml",
		"path to the YAML configuration file",
	)
	dryRun := flag.Bool(
		"dry-run",
		false,
		"fetch the first page, infer the Doris column schema, and print a CREATE TABLE DDL — nothing is written to Doris (also via DRY_RUN=1 env var)",
	)
	flag.Parse()

	// Also honour the DRY_RUN environment variable so that Docker / Kubernetes
	// users can activate dry-run mode with -e DRY_RUN=1 without having to
	// reconstruct the CMD arguments. The flag takes precedence when set explicitly.
	if !*dryRun {
		v := os.Getenv("DRY_RUN")
		*dryRun = v == "1" || strings.EqualFold(v, "true")
	}

	// Use structured JSON logging to stdout so log lines are parseable by
	// Kubernetes log aggregators (Loki, Fluentd, etc.).
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Respect SIGINT and SIGTERM for graceful shutdown (e.g. Kubernetes pod eviction).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", "path", *configPath, "error", err)
		os.Exit(1)
	}

	if err := config.ApplyEnvOverrides(cfg); err != nil {
		slog.Error("failed to apply environment variable overrides", "error", err)
		os.Exit(1)
	}

	r, err := runner.New(cfg)
	if err != nil {
		slog.Error("failed to initialise runner", "error", err)
		os.Exit(1)
	}

	if *dryRun {
		if err := r.RunDryRun(ctx); err != nil {
			slog.Error("dry-run failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := r.Run(ctx); err != nil {
		slog.Error("ingestion failed", "error", err)
		os.Exit(1)
	}

	slog.Info("ingestion completed successfully")
}
