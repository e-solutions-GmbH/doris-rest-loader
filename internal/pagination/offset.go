package pagination

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// Offset implements offset-based pagination.
// Each request specifies a record offset: offset = (pageNum-1) * PageSize.
type Offset struct {
	cfg config.PaginationConfig
}

func newOffset(cfg config.PaginationConfig) *Offset {
	return &Offset{cfg: cfg}
}

// Evaluate reads the total entry count and computes total pages.
func (o *Offset) Evaluate(resp map[string]any) (*PageInfo, error) {
	totalEntries, err := jsonpath.GetInt(resp, o.cfg.TotalEntriesPath)
	if err != nil {
		return nil, fmt.Errorf("pagination: read total_entries_path %q: %w", o.cfg.TotalEntriesPath, err)
	}

	totalPages := 0
	if totalEntries > 0 {
		totalPages = (totalEntries + o.cfg.PageSize - 1) / o.cfg.PageSize
	}

	return &PageInfo{
		TotalPages:   totalPages,
		TotalEntries: totalEntries,
		PageSize:     o.cfg.PageSize,
		StartPage:    1,
	}, nil
}

// BuildURL constructs the request URL for the given page number.
// The offset is calculated as (pageNum - 1) * PageSize.
//
// injection: path — replaces {offset_param} and {limit_param} placeholders in the URL path:
//
//	baseURL  = "https://api.example.com/v1/items/{offset}/{limit}"
//	pageNum  = 3, PageSize = 100
//	result   = "https://api.example.com/v1/items/200/100"
//
// injection: query_params — appends (or overwrites) offset_param and limit_param
// as URL query parameters, preserving any parameters already present in the URL:
//
//	baseURL  = "https://api.example.com/v1/search?jql=project=FOO"
//	pageNum  = 3, PageSize = 50, OffsetParam = "startAt", LimitParam = "maxResults"
//	result   = "https://api.example.com/v1/search?jql=project%3DFOO&maxResults=50&startAt=100"
func (o *Offset) BuildURL(baseURL string, pageNum int) (string, error) {
	offset := (pageNum - 1) * o.cfg.PageSize
	switch o.cfg.Injection {
	case "path":
		u := strings.ReplaceAll(baseURL, "{"+o.cfg.OffsetParam+"}", strconv.Itoa(offset))
		u = strings.ReplaceAll(u, "{"+o.cfg.LimitParam+"}", strconv.Itoa(o.cfg.PageSize))
		return u, nil
	case "query_params":
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("pagination: parse base URL: %w", err)
		}
		q := u.Query()
		q.Set(o.cfg.OffsetParam, strconv.Itoa(offset))
		q.Set(o.cfg.LimitParam, strconv.Itoa(o.cfg.PageSize))
		u.RawQuery = q.Encode()
		return u.String(), nil
	case "body":
		// TODO: Encode offset and limit in the request body (requires fetcher changes).
		return "", fmt.Errorf("pagination: body injection is not yet implemented")
	default:
		return "", fmt.Errorf("pagination: unknown injection type %q", o.cfg.Injection)
	}
}
