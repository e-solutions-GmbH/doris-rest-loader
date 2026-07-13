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
