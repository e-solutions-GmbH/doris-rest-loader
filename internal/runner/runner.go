// Package runner implements the top-level ingestion pipeline:
//
//  1. Authenticate (preflight if required)
//  2. Fetch the first page
//  3. Flatten entities and stream them to Doris
//  4. Evaluate pagination metadata
//  5. Spawn up to MaxGoroutines goroutines for remaining pages
//  6. Each goroutine fetches, flattens, and sends its result to a shared channel
//  7. A serial reader streams each page result to Doris
//  8. Return an aggregated error if any page failed
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/auth"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/doris"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/fetcher"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/flattener"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/pagination"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/schema"
)

// pageResult carries flattened entities for a single page, or an error.
type pageResult struct {
	pageNum  int
	entities []map[string]any
	err      error
}

// Runner orchestrates the full ingestion pipeline for one configured endpoint.
type Runner struct {
	cfg         *config.Config
	authAdapter auth.Adapter
	pagAdapter  pagination.Adapter
	fetcher     *fetcher.Fetcher
	flat        *flattener.Flattener
	loader      *doris.StreamLoader
}

// New constructs a Runner, initialising all adapters from the configuration.
// Returns an error if any adapter cannot be created (e.g. unknown auth type).
func New(cfg *config.Config) (*Runner, error) {
	authAdapter, err := auth.NewAdapter(cfg.Auth, cfg.Source.TLSSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("runner: init auth adapter: %w", err)
	}

	pagAdapter, err := pagination.NewAdapter(cfg.Pagination)
	if err != nil {
		return nil, fmt.Errorf("runner: init pagination adapter: %w", err)
	}

	return &Runner{
		cfg:         cfg,
		authAdapter: authAdapter,
		pagAdapter:  pagAdapter,
		fetcher:     fetcher.New(cfg.Source, cfg.Threading.Retry, authAdapter),
		flat:        flattener.New(cfg.Flattening),
		loader:      doris.NewStreamLoader(cfg.Doris),
	}, nil
}

// Run executes the full ingestion pipeline and returns any accumulated errors.
func (r *Runner) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "starting ingestion",
		"url", r.cfg.Source.URL,
		"doris_target", fmt.Sprintf("%s/api/%s/%s/_stream_load",
			strings.TrimRight(r.cfg.Doris.Host, "/"), r.cfg.Doris.Database, r.cfg.Doris.Table),
	)

	// Step 1: pre-flight authentication
	if err := r.authAdapter.Prepare(ctx); err != nil {
		return fmt.Errorf("runner: auth prepare: %w", err)
	}

	// Step 2: fetch first page
	firstURL, err := r.pagAdapter.BuildURL(r.cfg.Source.URL, r.cfg.Pagination.StartPage)
	if err != nil {
		return fmt.Errorf("runner: build first page URL: %w", err)
	}

	slog.InfoContext(ctx, "fetching first page", "url", firstURL)
	firstRaw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return fmt.Errorf("runner: fetch first page: %w", err)
	}

	// Step 3: extract, flatten, and stream first page
	firstEntities, err := extractEntities(firstRaw, r.cfg.Source.DataPath)
	if err != nil {
		return fmt.Errorf("runner: extract entities from first page: %w", err)
	}

	flatFirst, err := r.flat.FlattenAll(firstEntities)
	if err != nil {
		return fmt.Errorf("runner: flatten first page: %w", err)
	}

	slog.InfoContext(ctx, "streaming first page to Doris", "entity_count", len(flatFirst))
	if err := r.loader.LoadPage(ctx, flatFirst); err != nil {
		return fmt.Errorf("runner: load first page to Doris: %w", err)
	}

	// Step 4: evaluate pagination
	pageInfo, err := r.evaluatePagination(firstRaw)
	if err != nil {
		// Array-at-root or missing metadata → treat as single page, done.
		slog.InfoContext(ctx, "pagination not available, treating as single page",
			"reason", err.Error())
		return nil
	}

	slog.InfoContext(ctx, "pagination evaluated",
		"total_pages", pageInfo.TotalPages,
		"total_entries", pageInfo.TotalEntries,
		"page_size", pageInfo.PageSize,
	)

	if pageInfo.TotalPages <= 1 {
		slog.InfoContext(ctx, "single page result, ingestion complete")
		return nil
	}

	// Steps 5–8: parallel fetch and stream of remaining pages
	return r.fetchAndStreamRemaining(ctx, pageInfo)
}

// RunDryRun executes the authentication preflight and fetches only the first
// page of data. It then runs the full flattening pipeline and prints an
// inferred CREATE TABLE DDL to stdout — without writing anything to Doris.
//
// Use this mode to inspect the exact column structure before creating the
// target Doris table:
//
//	doris-rest-loader --config ./config.yaml --dry-run
func (r *Runner) RunDryRun(ctx context.Context) error {
	slog.InfoContext(ctx, "dry-run mode: fetching first page to infer schema",
		"url", r.cfg.Source.URL,
		"doris_target", fmt.Sprintf("%s/api/%s/%s/_stream_load",
			strings.TrimRight(r.cfg.Doris.Host, "/"), r.cfg.Doris.Database, r.cfg.Doris.Table),
	)

	// Step 1: preflight authentication (needed for protected endpoints).
	if err := r.authAdapter.Prepare(ctx); err != nil {
		return fmt.Errorf("runner: auth prepare: %w", err)
	}

	// Step 2: fetch only the first page.
	firstURL, err := r.pagAdapter.BuildURL(r.cfg.Source.URL, r.cfg.Pagination.StartPage)
	if err != nil {
		return fmt.Errorf("runner: build first page URL: %w", err)
	}

	slog.InfoContext(ctx, "dry-run: fetching first page", "url", firstURL)
	firstRaw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return fmt.Errorf("runner: fetch first page: %w", err)
	}

	// Step 3: extract and flatten — same path as a real run.
	firstEntities, err := extractEntities(firstRaw, r.cfg.Source.DataPath)
	if err != nil {
		return fmt.Errorf("runner: extract entities from first page: %w", err)
	}

	flatEntities, err := r.flat.FlattenAll(firstEntities)
	if err != nil {
		return fmt.Errorf("runner: flatten first page: %w", err)
	}

	slog.InfoContext(ctx, "dry-run: inferring schema",
		"entity_count", len(flatEntities),
	)

	// Infer schema and print DDL — nothing is written to Doris.
	cols := schema.Infer(flatEntities)
	fmt.Println(schema.DDL(r.cfg.Doris.Table, cols))
	return nil
}

// evaluatePagination attempts to read pagination metadata from the raw response.
// Returns an error (non-fatal) when the response is a root-level array, since
// array responses carry no pagination metadata.
func (r *Runner) evaluatePagination(raw any) (*pagination.PageInfo, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response root is a JSON array; no pagination metadata available")
	}
	return r.pagAdapter.Evaluate(obj)
}

// fetchAndStreamRemaining spawns goroutines for pages [startPage+1 … totalPages],
// streams each completed page to Doris via a channel, and returns an aggregate
// error if any page failed.
func (r *Runner) fetchAndStreamRemaining(ctx context.Context, pageInfo *pagination.PageInfo) error {
	results := make(chan pageResult, r.cfg.Threading.MaxGoroutines)
	sem := make(chan struct{}, r.cfg.Threading.MaxGoroutines)

	// Producer/consumer pipeline: the dispatch loop runs in its own goroutine
	// so this function can drain `results` concurrently. `sem` caps in-flight
	// fetches at MaxGoroutines; `results` is buffered to the same size so a
	// slow consumer applies back-pressure without starving the pool.
	var wg sync.WaitGroup
	go func() {
		for pageNum := pageInfo.StartPage + 1; pageNum <= pageInfo.TotalPages; pageNum++ {
			wg.Add(1)
			sem <- struct{}{} // acquire concurrency slot

			go func(pn int) {
				defer wg.Done()
				defer func() { <-sem }() // release slot when done

				results <- r.fetchPage(ctx, pn)
			}(pageNum)
		}
		// Close the results channel once every worker has finished.
		wg.Wait()
		close(results)
	}()

	// Read results as they arrive and stream each to Doris.
	var errs []error
	for res := range results {
		if res.err != nil {
			slog.ErrorContext(ctx, "page fetch/flatten failed",
				"page", res.pageNum, "error", res.err)
			errs = append(errs, res.err)
			continue
		}
		slog.InfoContext(ctx, "streaming page to Doris",
			"page", res.pageNum, "entity_count", len(res.entities))
		if err := r.loader.LoadPage(ctx, res.entities); err != nil {
			slog.ErrorContext(ctx, "Doris load failed",
				"page", res.pageNum, "error", err)
			errs = append(errs, fmt.Errorf("page %d: %w", res.pageNum, err))
		}
	}

	return errors.Join(errs...)
}

// fetchPage fetches a single page, extracts its entities, flattens them,
// and returns a pageResult (which may carry an error instead of entities).
func (r *Runner) fetchPage(ctx context.Context, pageNum int) pageResult {
	pageURL, err := r.pagAdapter.BuildURL(r.cfg.Source.URL, pageNum)
	if err != nil {
		return pageResult{pageNum: pageNum,
			err: fmt.Errorf("build URL for page %d: %w", pageNum, err)}
	}

	raw, err := r.fetcher.FetchWithRetry(ctx, pageURL)
	if err != nil {
		return pageResult{pageNum: pageNum,
			err: fmt.Errorf("fetch page %d: %w", pageNum, err)}
	}

	entities, err := extractEntities(raw, r.cfg.Source.DataPath)
	if err != nil {
		return pageResult{pageNum: pageNum,
			err: fmt.Errorf("extract entities from page %d: %w", pageNum, err)}
	}

	flatEntities, err := r.flat.FlattenAll(entities)
	if err != nil {
		return pageResult{pageNum: pageNum,
			err: fmt.Errorf("flatten page %d: %w", pageNum, err)}
	}

	return pageResult{pageNum: pageNum, entities: flatEntities}
}

// Helpers:
//
// extractEntities navigates to dataPath within raw and converts the result to
// []map[string]any. When dataPath is empty the response root is used.
//
// Handles two root shapes:
//   - []any  → each element must be a JSON object
//   - map[string]any → treated as a single-entity response
func extractEntities(raw any, dataPath string) ([]map[string]any, error) {
	target := raw

	if dataPath != "" {
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"response root is not a JSON object; cannot navigate to data_path %q", dataPath)
		}
		val, err := jsonpath.Get(obj, dataPath)
		if err != nil {
			return nil, fmt.Errorf("data_path %q: %w", dataPath, err)
		}
		target = val
	}

	switch v := target.(type) {
	case []any:
		return anySliceToEntities(v)
	case map[string]any:
		return []map[string]any{v}, nil
	default:
		return nil, fmt.Errorf(
			"data at path %q is neither a JSON array nor a JSON object (got %T)", dataPath, target)
	}
}

// anySliceToEntities converts []any to []map[string]any.
// Returns an error if any element is not a JSON object.
func anySliceToEntities(arr []any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(arr))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("array element %d is not a JSON object (got %T)", i, item)
		}
		out = append(out, m)
	}
	return out, nil
}
