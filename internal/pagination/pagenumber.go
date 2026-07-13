package pagination

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// PageNumber implements page-number based pagination, where each request
// specifies an explicit page index (e.g. ?page=2&limit=100).
//
// Total page count is derived from either:
//   - num_pages_path  — a field in the response that contains the total page count directly, or
//   - total_entries_path + page_size — computed as ceil(totalEntries / pageSize).
type PageNumber struct {
	cfg config.PaginationConfig
}

func newPageNumber(cfg config.PaginationConfig) *PageNumber {
	return &PageNumber{cfg: cfg}
}

// Evaluate reads total page information from the first-page response.
func (p *PageNumber) Evaluate(resp map[string]any) (*PageInfo, error) {
	if p.cfg.NumPagesPath != "" {
		totalPages, err := jsonpath.GetInt(resp, p.cfg.NumPagesPath)
		if err != nil {
			return nil, fmt.Errorf("pagination: read num_pages_path %q: %w", p.cfg.NumPagesPath, err)
		}
		return &PageInfo{
			TotalPages: totalPages,
			PageSize:   p.cfg.PageSize,
			StartPage:  p.cfg.StartPage,
		}, nil
	}

	// Fall back to deriving total pages from total entry count.
	totalEntries, err := jsonpath.GetInt(resp, p.cfg.TotalEntriesPath)
	if err != nil {
		return nil, fmt.Errorf("pagination: read total_entries_path %q: %w", p.cfg.TotalEntriesPath, err)
	}

	totalPages := 0
	if totalEntries > 0 {
		totalPages = (totalEntries + p.cfg.PageSize - 1) / p.cfg.PageSize
	}

	return &PageInfo{
		TotalPages:   totalPages,
		TotalEntries: totalEntries,
		PageSize:     p.cfg.PageSize,
		StartPage:    p.cfg.StartPage,
	}, nil
}

// BuildURL constructs the request URL for the given page number.
//
// injection: path — replaces {page_param} and {limit_param} placeholders in the URL path:
//
//	baseURL  = "https://api.example.com/v1/items/{page}/{limit}"
//	pageNum  = 3, PageSize = 100
//	result   = "https://api.example.com/v1/items/3/100"
//
// injection: query_params — appends (or overwrites) page_param and limit_param
// as URL query parameters, preserving any parameters already present in the URL:
//
//	baseURL  = "https://api.example.com/v1/items?filter=active"
//	pageNum  = 3, PageSize = 100, PageParam = "page", LimitParam = "limit"
//	result   = "https://api.example.com/v1/items?filter=active&limit=100&page=3"
func (p *PageNumber) BuildURL(baseURL string, pageNum int) (string, error) {
	switch p.cfg.Injection {
	case "path":
		u := strings.ReplaceAll(baseURL, "{"+p.cfg.PageParam+"}", strconv.Itoa(pageNum))
		u = strings.ReplaceAll(u, "{"+p.cfg.LimitParam+"}", strconv.Itoa(p.cfg.PageSize))
		return u, nil
	case "query_params":
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("pagination: parse base URL: %w", err)
		}
		q := u.Query()
		q.Set(p.cfg.PageParam, strconv.Itoa(pageNum))
		q.Set(p.cfg.LimitParam, strconv.Itoa(p.cfg.PageSize))
		u.RawQuery = q.Encode()
		return u.String(), nil
	case "body":
		// TODO: Encode page and limit in the request body (requires fetcher changes).
		return "", fmt.Errorf("pagination: body injection is not yet implemented")
	default:
		return "", fmt.Errorf("pagination: unknown injection type %q", p.cfg.Injection)
	}
}
