package pagination

import (
	"fmt"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// Cursor is a stub for keyset / cursor-based pagination.
//
// Keyset pagination (also called cursor pagination) works by using a
// server-provided opaque cursor value from the current page response to
// request the next page. This avoids the "offset drift" problem of
// offset-based pagination and is efficient on large datasets because the
// database can seek directly to the next keyset position.
//
// Typical flow:
//  1. Client fetches page 1 with no cursor.
//  2. Server returns results and a "nextCursor" field in the response.
//  3. Client sends the cursor value as a query parameter or path segment
//     to fetch page 2.
//  4. Repeat until "nextCursor" is absent or null, indicating the last page.
//
// Because cursor pagination is inherently sequential (each cursor depends on
// the previous page response), concurrent goroutine fetching is not applicable.
// The threading model must be adapted accordingly when this is implemented.
//
// TODO: Implement cursor-based pagination.
type Cursor struct {
	cfg config.PaginationConfig //nolint:unused
}

func newCursor(cfg config.PaginationConfig) *Cursor {
	return &Cursor{cfg: cfg}
}

// Evaluate is not implemented for cursor-based pagination.
func (c *Cursor) Evaluate(_ map[string]any) (*PageInfo, error) {
	return nil, fmt.Errorf("pagination: cursor-based pagination is not yet implemented")
}

// BuildURL is not implemented for cursor-based pagination.
func (c *Cursor) BuildURL(_ string, _ int) (string, error) {
	return "", fmt.Errorf("pagination: cursor-based pagination is not yet implemented")
}
