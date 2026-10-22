// pagewalk.go implements a single, generic sequential page-walking helper
// used by every pagination consumer that must — or already does — walk
// pages one at a time rather than concurrently: the fan-out list fetch, the
// optional per-item sub-list fetch, the per-item detail fetch, and (for
// cursor/sequential adapters only) the top-level single-endpoint fetch.
//
// Counted adapters (page_number, offset) are walked by evaluating total page
// count from the first page's response and then building each subsequent
// page's URL directly from its page number. Cursor/sequential adapters
// (pagination.SequentialAdapter) are walked by asking the adapter for the
// next page's URL based on the current page's response body — this is the
// only way cursor pagination can work, since each page's URL is not known
// ahead of time.
//
// The top-level single-endpoint fetch's existing CONCURRENT page-fetching
// path (fetchAndStreamRemaining/fetchPage, used for counted adapters) is
// deliberately left untouched and does not go through this helper — it
// remains the fast path for the common case. Only Run()'s cursor-adapter
// branch (runSequential) uses sequentialPages, since cursor pagination
// cannot be parallelized at all.
package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/pagination"
)

// sequentialPages walks every page of a paginated endpoint, starting at
// baseURL, invoking onPage once per page (in order, 1-based page numbers)
// with that page's extracted — but not flattened — entities.
//
// adapter may be nil, meaning "no pagination configured": baseURL is fetched
// exactly once and onPage is called once with page number 1. Otherwise
// adapter drives pagination: counted adapters (page_number, offset) via
// Evaluate+BuildURL(pageNum); sequential/cursor adapters
// (pagination.SequentialAdapter) via BuildURL(baseURL, StartPage) for the
// first page and NextPageURL for every subsequent page.
//
// pagCfg.MaxPages, when > 0, caps the total number of pages fetched,
// regardless of which adapter kind is in play.
//
// If onPage returns an error, the walk stops immediately and that error is
// returned (wrapped with page context by the caller's onPage, if desired).
func (r *Runner) sequentialPages(
	ctx context.Context,
	adapter pagination.Adapter,
	pagCfg config.PaginationConfig,
	baseURL string,
	dataPath string,
	onPage func(pageNum int, entities []map[string]any) error,
) error {
	startPage := 1
	firstURL := baseURL
	if adapter != nil {
		startPage = pagCfg.StartPage
		u, err := adapter.BuildURL(baseURL, startPage)
		if err != nil {
			return fmt.Errorf("build first page URL: %w", err)
		}
		firstURL = u
	}

	slog.InfoContext(ctx, "fetching page", "page", startPage, "url", firstURL)
	raw, err := r.fetcher.FetchWithRetry(ctx, firstURL)
	if err != nil {
		return fmt.Errorf("fetch page %d: %w", startPage, err)
	}

	entities, err := extractEntities(raw, dataPath)
	if err != nil {
		return fmt.Errorf("extract entities from page %d: %w", startPage, err)
	}
	if err := onPage(startPage, entities); err != nil {
		return err
	}

	if adapter == nil {
		return nil
	}

	if seqAdapter, ok := adapter.(pagination.SequentialAdapter); ok {
		return r.sequentialPagesCursor(ctx, seqAdapter, pagCfg, baseURL, dataPath, startPage, raw, onPage)
	}
	return r.sequentialPagesCounted(ctx, adapter, pagCfg, baseURL, dataPath, startPage, raw, onPage)
}

// sequentialPagesCounted walks the remaining pages of a counted adapter
// (page_number, offset) after the first page (firstRaw) has already been
// fetched and passed to onPage.
func (r *Runner) sequentialPagesCounted(
	ctx context.Context,
	adapter pagination.Adapter,
	pagCfg config.PaginationConfig,
	baseURL string,
	dataPath string,
	startPage int,
	firstRaw any,
	onPage func(pageNum int, entities []map[string]any) error,
) error {
	obj, ok := firstRaw.(map[string]any)
	if !ok {
		// Array-at-root response carries no pagination metadata; treat as a
		// single page, done — same convention used throughout the pipeline.
		return nil
	}

	pageInfo, err := adapter.Evaluate(obj)
	if err != nil {
		// Pagination metadata not available/applicable: single page, done.
		return nil
	}

	if maxPages := pagCfg.MaxPages; maxPages > 0 && pageInfo.TotalPages > maxPages {
		pageInfo.TotalPages = maxPages
	}

	for pageNum := startPage + 1; pageNum <= pageInfo.TotalPages; pageNum++ {
		pageURL, err := adapter.BuildURL(baseURL, pageNum)
		if err != nil {
			return fmt.Errorf("build URL for page %d: %w", pageNum, err)
		}

		slog.InfoContext(ctx, "fetching page", "page", pageNum, "url", pageURL)
		raw, err := r.fetcher.FetchWithRetry(ctx, pageURL)
		if err != nil {
			return fmt.Errorf("fetch page %d: %w", pageNum, err)
		}

		entities, err := extractEntities(raw, dataPath)
		if err != nil {
			return fmt.Errorf("extract entities from page %d: %w", pageNum, err)
		}
		if err := onPage(pageNum, entities); err != nil {
			return err
		}
	}
	return nil
}

// sequentialPagesCursor walks the remaining pages of a sequential/cursor
// adapter after the first page (firstRaw) has already been fetched and
// passed to onPage. Each subsequent page's URL is derived from the previous
// page's response body via NextPageURL — this cannot be parallelized.
func (r *Runner) sequentialPagesCursor(
	ctx context.Context,
	adapter pagination.SequentialAdapter,
	pagCfg config.PaginationConfig,
	baseURL string,
	dataPath string,
	startPage int,
	firstRaw any,
	onPage func(pageNum int, entities []map[string]any) error,
) error {
	currentRaw := firstRaw
	pageNum := startPage
	pagesFetched := 1

	for {
		obj, ok := currentRaw.(map[string]any)
		if !ok {
			// Array-at-root response carries no pagination metadata; treat
			// as a single page, done.
			return nil
		}

		if maxPages := pagCfg.MaxPages; maxPages > 0 && pagesFetched >= maxPages {
			return nil
		}

		nextURL, hasNext, err := adapter.NextPageURL(baseURL, obj)
		if err != nil {
			return fmt.Errorf("determine next page after page %d: %w", pageNum, err)
		}
		if !hasNext {
			return nil
		}
		pageNum++

		slog.InfoContext(ctx, "fetching page", "page", pageNum, "url", nextURL)
		raw, err := r.fetcher.FetchWithRetry(ctx, nextURL)
		if err != nil {
			return fmt.Errorf("fetch page %d: %w", pageNum, err)
		}

		entities, err := extractEntities(raw, dataPath)
		if err != nil {
			return fmt.Errorf("extract entities from page %d: %w", pageNum, err)
		}
		if err := onPage(pageNum, entities); err != nil {
			return err
		}

		pagesFetched++
		currentRaw = raw
	}
}
