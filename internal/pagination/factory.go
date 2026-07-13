package pagination

import (
	"fmt"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// NewAdapter creates the appropriate pagination Adapter for the given configuration.
func NewAdapter(cfg config.PaginationConfig) (Adapter, error) {
	switch cfg.Type {
	case "page_number", "":
		return newPageNumber(cfg), nil
	case "offset":
		return newOffset(cfg), nil
	case "cursor":
		return newCursor(cfg), nil
	default:
		return nil, fmt.Errorf("pagination: unknown type %q (supported: page_number, offset, cursor)", cfg.Type)
	}
}
