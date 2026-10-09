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
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/fanout"
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
	// listPagAdapter paginates the fan-out list fetch (source.fanout.list_pagination).
	// nil unless source.fanout is configured with a non-empty list_pagination block.
	listPagAdapter pagination.Adapter
	fetcher        *fetcher.Fetcher
	flat           *flattener.Flattener
	loader         *doris.StreamLoader
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

	var listPagAdapter pagination.Adapter
	if cfg.Source.FanOut != nil && !cfg.Source.FanOut.ListPagination.IsZero() {
		listPagAdapter, err = pagination.NewAdapter(cfg.Source.FanOut.ListPagination)
		if err != nil {
			return nil, fmt.Errorf("runner: init fan-out list pagination adapter: %w", err)
		}
	}

	return &Runner{
		cfg:            cfg,
		authAdapter:    authAdapter,
		pagAdapter:     pagAdapter,
		listPagAdapter: listPagAdapter,
		fetcher:        fetcher.New(cfg.Source, cfg.Threading.Retry, authAdapter),
		flat:           flattener.New(cfg.Flattening),
		loader:         doris.NewStreamLoader(cfg.Doris),
	}, nil
}

// Run executes the full ingestion pipeline and returns any accumulated errors.
func (r *Runner) Run(ctx context.Context) error {
	if r.cfg.Source.FanOut != nil {
		return r.runFanOut(ctx)
	}

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

	// Apply optional MaxPages cap — limits pages fetched regardless of source size (value of 0 implies not caped and unlimited).
	if maxPages := r.cfg.Pagination.MaxPages; maxPages > 0 && pageInfo.TotalPages > maxPages {
		slog.InfoContext(ctx, "max_pages cap applied",
			"source_total_pages", pageInfo.TotalPages,
			"max_pages", maxPages,
		)
		pageInfo.TotalPages = maxPages
	}

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
	if r.cfg.Source.FanOut != nil {
		return r.runFanOutDryRun(ctx)
	}

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

// ─── Fan-out ────────────────────────────────────────────────────────────────
//
// runFanOut implements the two-stage "list, then fan out a parameterised call
// per list entry" source mode (source.fanout):
//
//  1. Fetch the list endpoint (optionally paginated via list_pagination),
//     reusing the same pagination.Adapter / fetcher.Fetcher / extractEntities
//     building blocks as the single-endpoint path.
//  2. Extract a deduplicated set of fan-out items from the list entities.
//  3. For each item, substitute it into source.url and run the existing
//     single-endpoint pipeline (fetch, extract, flatten, stream to Doris),
//     independently paginated via the top-level `pagination` block.
//
// Per-item fetches run concurrently, bounded by threading.max_goroutines; a
// single failing item is logged and skipped rather than aborting the run.
func (r *Runner) runFanOut(ctx context.Context) error {
	fo := r.cfg.Source.FanOut

	slog.InfoContext(ctx, "starting fan-out ingestion",
		"list_url", fo.ListURL,
		"url_template", r.cfg.Source.URL,
		"doris_target", fmt.Sprintf("%s/api/%s/%s/_stream_load",
			strings.TrimRight(r.cfg.Doris.Host, "/"), r.cfg.Doris.Database, r.cfg.Doris.Table),
	)

	if err := r.authAdapter.Prepare(ctx); err != nil {
		return fmt.Errorf("runner: auth prepare: %w", err)
	}

	listEntities, err := r.fetchAllListEntities(ctx)
	if err != nil {
		return fmt.Errorf("runner: fan-out list fetch: %w", err)
	}

	items, err := fanout.ExtractItems(listEntities, fo.ItemField)
	if err != nil {
		return fmt.Errorf("runner: fan-out item extraction: %w", err)
	}

	slog.InfoContext(ctx, "fan-out list fetch complete",
		"list_entity_count", len(listEntities),
		"item_count", len(items),
	)

	if len(items) == 0 {
		slog.InfoContext(ctx, "fan-out: no items to process, ingestion complete")
		return nil
	}

	return r.fetchAndStreamFanOutItems(ctx, items)
}

// runFanOutDryRun mirrors RunDryRun for the fan-out mode: it fetches the list
// (stage 1), takes only the first fan-out item, fetches that item's first
// detail page (stage 2, first page only), and prints the inferred DDL —
// nothing is written to Doris.
func (r *Runner) runFanOutDryRun(ctx context.Context) error {
	fo := r.cfg.Source.FanOut

	slog.InfoContext(ctx, "dry-run mode (fan-out): fetching list to select first item",
		"list_url", fo.ListURL,
		"doris_target", fmt.Sprintf("%s/api/%s/%s/_stream_load",
			strings.TrimRight(r.cfg.Doris.Host, "/"), r.cfg.Doris.Database, r.cfg.Doris.Table),
	)

	if err := r.authAdapter.Prepare(ctx); err != nil {
		return fmt.Errorf("runner: auth prepare: %w", err)
	}

	listEntities, err := r.fetchAllListEntities(ctx)
	if err != nil {
		return fmt.Errorf("runner: fan-out list fetch: %w", err)
	}

	items, err := fanout.ExtractItems(listEntities, fo.ItemField)
	if err != nil {
		return fmt.Errorf("runner: fan-out item extraction: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("runner: fan-out list fetch returned no items, cannot dry-run")
	}

	firstItem := items[0]
	slog.InfoContext(ctx, "dry-run (fan-out): using first item", "item", firstItem)

	itemURL := fanout.BuildItemURL(r.cfg.Source.URL, fo.ItemPlaceholder, firstItem)

	firstURL, err := r.pagAdapter.BuildURL(itemURL, r.cfg.Pagination.StartPage)
	if err != nil {
		return fmt.Errorf("runner: build first page URL: %w", err)
	}

	slog.InfoContext(ctx, "dry-run (fan-out): fetching first item's first page", "url", firstURL)
	firstRaw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return fmt.Errorf("runner: fetch first page: %w", err)
	}

	firstEntities, err := extractEntities(firstRaw, r.cfg.Source.DataPath)
	if err != nil {
		return fmt.Errorf("runner: extract entities from first page: %w", err)
	}
	fanout.InjectItemColumn(firstEntities, fo.ItemColumn, firstItem)

	flatEntities, err := r.flat.FlattenAll(firstEntities)
	if err != nil {
		return fmt.Errorf("runner: flatten first page: %w", err)
	}

	slog.InfoContext(ctx, "dry-run (fan-out): inferring schema", "entity_count", len(flatEntities))

	cols := schema.Infer(flatEntities)
	fmt.Println(schema.DDL(r.cfg.Doris.Table, cols))
	return nil
}

// fetchAllListEntities fetches the fan-out list endpoint — optionally across
// multiple pages when source.fanout.list_pagination is configured — and
// returns the concatenated list entities across all list pages.
func (r *Runner) fetchAllListEntities(ctx context.Context) ([]map[string]any, error) {
	fo := r.cfg.Source.FanOut

	firstURL := fo.ListURL
	if r.listPagAdapter != nil {
		u, err := r.listPagAdapter.BuildURL(fo.ListURL, fo.ListPagination.StartPage)
		if err != nil {
			return nil, fmt.Errorf("build list URL: %w", err)
		}
		firstURL = u
	}

	slog.InfoContext(ctx, "fan-out: fetching list page", "url", firstURL)
	firstRaw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return nil, fmt.Errorf("fetch list page: %w", err)
	}

	entities, err := extractEntities(firstRaw, fo.ListDataPath)
	if err != nil {
		return nil, fmt.Errorf("extract list entities: %w", err)
	}

	// No list pagination configured: the list endpoint returns everything in
	// a single response.
	if r.listPagAdapter == nil {
		return entities, nil
	}

	obj, ok := firstRaw.(map[string]any)
	if !ok {
		// Array-at-root list response carries no pagination metadata; treat
		// as a single page, same convention as the top-level pipeline.
		return entities, nil
	}
	pageInfo, err := r.listPagAdapter.Evaluate(obj)
	if err != nil {
		// Pagination metadata not available/applicable: single page, done.
		return entities, nil
	}

	for pageNum := pageInfo.StartPage + 1; pageNum <= pageInfo.TotalPages; pageNum++ {
		pageURL, err := r.listPagAdapter.BuildURL(fo.ListURL, pageNum)
		if err != nil {
			return nil, fmt.Errorf("build list URL for page %d: %w", pageNum, err)
		}

		slog.InfoContext(ctx, "fan-out: fetching list page", "url", pageURL, "page", pageNum)
		raw, err := r.fetcher.FetchWithRetry(ctx, pageURL)
		if err != nil {
			return nil, fmt.Errorf("fetch list page %d: %w", pageNum, err)
		}

		pageEntities, err := extractEntities(raw, fo.ListDataPath)
		if err != nil {
			return nil, fmt.Errorf("extract list entities from page %d: %w", pageNum, err)
		}
		entities = append(entities, pageEntities...)
	}

	return entities, nil
}

// fanOutItemResult carries the outcome of processing a single fan-out item.
type fanOutItemResult struct {
	item string
	err  error
}

// fetchAndStreamFanOutItems dispatches one goroutine per fan-out item, bounded
// by threading.max_goroutines (reusing the same semaphore/channel pattern as
// fetchAndStreamRemaining, applied across items instead of across pages).
// Each item's own detail pages are fetched and streamed sequentially within
// that item's goroutine — concurrency is only applied across items, not
// nested across an item's pages. A failing item is logged and skipped; the
// aggregate error (if any) is returned once every item has been attempted.
func (r *Runner) fetchAndStreamFanOutItems(ctx context.Context, items []string) error {
	results := make(chan fanOutItemResult, r.cfg.Threading.MaxGoroutines)
	sem := make(chan struct{}, r.cfg.Threading.MaxGoroutines)

	var wg sync.WaitGroup
	go func() {
		for _, item := range items {
			wg.Add(1)
			sem <- struct{}{} // acquire concurrency slot

			go func(it string) {
				defer wg.Done()
				defer func() { <-sem }() // release slot when done

				slog.InfoContext(ctx, "fan-out: processing item", "item", it)
				results <- fanOutItemResult{item: it, err: r.processFanOutItem(ctx, it)}
			}(item)
		}
		wg.Wait()
		close(results)
	}()

	var errs []error
	succeeded := 0
	for res := range results {
		if res.err != nil {
			slog.ErrorContext(ctx, "fan-out item failed", "item", res.item, "error", res.err)
			errs = append(errs, fmt.Errorf("item %q: %w", res.item, res.err))
			continue
		}
		succeeded++
	}

	slog.InfoContext(ctx, "fan-out summary",
		"items_attempted", len(items),
		"items_succeeded", succeeded,
		"items_failed", len(errs),
	)

	return errors.Join(errs...)
}

// processFanOutItem runs the existing single-endpoint pipeline for one
// fan-out item: build the item's detail URL, fetch/flatten/stream its first
// page, then (if the detail endpoint is itself paginated) sequentially
// fetch/flatten/stream the remaining pages for that item.
func (r *Runner) processFanOutItem(ctx context.Context, item string) error {
	itemURL := fanout.BuildItemURL(r.cfg.Source.URL, r.cfg.Source.FanOut.ItemPlaceholder, item)

	firstURL, err := r.pagAdapter.BuildURL(itemURL, r.cfg.Pagination.StartPage)
	if err != nil {
		return fmt.Errorf("build first page URL: %w", err)
	}

	slog.InfoContext(ctx, "fan-out: fetching item's first page", "item", item, "url", firstURL)
	firstRaw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return fmt.Errorf("fetch first page: %w", err)
	}

	firstEntities, err := extractEntities(firstRaw, r.cfg.Source.DataPath)
	if err != nil {
		return fmt.Errorf("extract entities from first page: %w", err)
	}
	fanout.InjectItemColumn(firstEntities, r.cfg.Source.FanOut.ItemColumn, item)

	flatFirst, err := r.flat.FlattenAll(firstEntities)
	if err != nil {
		return fmt.Errorf("flatten first page: %w", err)
	}

	slog.InfoContext(ctx, "fan-out: streaming item's first page to Doris",
		"item", item, "entity_count", len(flatFirst))
	if err := r.loader.LoadPage(ctx, flatFirst); err != nil {
		return fmt.Errorf("load first page to Doris: %w", err)
	}

	pageInfo, err := r.evaluatePagination(firstRaw)
	if err != nil {
		// Array-at-root or missing metadata → treat as single page, done.
		return nil
	}

	if maxPages := r.cfg.Pagination.MaxPages; maxPages > 0 && pageInfo.TotalPages > maxPages {
		pageInfo.TotalPages = maxPages
	}

	for pageNum := pageInfo.StartPage + 1; pageNum <= pageInfo.TotalPages; pageNum++ {
		res := r.fetchItemPage(ctx, itemURL, item, pageNum)
		if res.err != nil {
			return fmt.Errorf("item page %d: %w", pageNum, res.err)
		}
		slog.InfoContext(ctx, "fan-out: streaming item page to Doris",
			"item", item, "page", pageNum, "entity_count", len(res.entities))
		if err := r.loader.LoadPage(ctx, res.entities); err != nil {
			return fmt.Errorf("load page %d to Doris: %w", pageNum, err)
		}
	}

	return nil
}

// fetchItemPage fetches a single detail page for a fan-out item, extracts its
// entities, flattens them, and returns a pageResult (which may carry an error
// instead of entities). itemURL is the already item-substituted base URL for
// this item (before pagination placeholders are applied). item is the raw
// fan-out item value, used to inject fanout.item_column (when configured)
// into every entity extracted from this page.
func (r *Runner) fetchItemPage(ctx context.Context, itemURL, item string, pageNum int) pageResult {
	pageURL, err := r.pagAdapter.BuildURL(itemURL, pageNum)
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
	fanout.InjectItemColumn(entities, r.cfg.Source.FanOut.ItemColumn, item)

	flatEntities, err := r.flat.FlattenAll(entities)
	if err != nil {
		return pageResult{pageNum: pageNum,
			err: fmt.Errorf("flatten page %d: %w", pageNum, err)}
	}

	return pageResult{pageNum: pageNum, entities: flatEntities}
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
