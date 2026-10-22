// Package pagination provides adapters for navigating paginated REST responses.
// Each adapter can evaluate total page count from a first-page response and
// construct the URL for any subsequent page.
package pagination

// PageInfo holds the pagination metadata derived from the first-page response.
type PageInfo struct {
	TotalPages   int
	TotalEntries int
	PageSize     int
	// StartPage is the 1-based index of the first page that was already fetched.
	StartPage int
}

// Adapter evaluates pagination metadata from a response and constructs page URLs.
type Adapter interface {
	// Evaluate parses the first-page response object and returns pagination details.
	// The supplied map must be the top-level JSON object from the first API response.
	Evaluate(firstResponse map[string]any) (*PageInfo, error)

	// BuildURL constructs the request URL for the given page number.
	// pageNum is always 1-based: page 1 is the first page, page 2 the second, etc.
	// For offset-based pagination, the adapter converts the page number to the
	// corresponding byte/record offset internally.
	BuildURL(baseURL string, pageNum int) (string, error)
}

// SequentialAdapter is implemented by pagination adapters whose page URLs
// cannot be known ahead of time — each page's URL depends on the previous
// page's response (e.g. cursor-based pagination, since the next cursor
// value only becomes known once the current page's response body has been
// parsed). Adapters implementing SequentialAdapter must be walked one page
// at a time, never concurrently: a counted adapter's Evaluate + per-page
// BuildURL(baseURL, pageNum) protocol does not apply to them (Evaluate
// returns an error), so callers must type-assert Adapter values to
// SequentialAdapter and route to a dedicated sequential page walk instead of
// the concurrent fetch-pool used for counted adapters (page_number, offset).
type SequentialAdapter interface {
	Adapter

	// NextPageURL inspects currentPageResponse (the top-level JSON object of
	// the page that was just fetched) and returns the URL for the next page.
	// hasNext is false when pagination is complete (there is no next page);
	// in that case url is unspecified and must not be used.
	NextPageURL(baseURL string, currentPageResponse map[string]any) (url string, hasNext bool, err error)
}
