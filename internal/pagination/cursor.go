package pagination

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// Cursor implements keyset / cursor-based pagination.
//
// Keyset pagination (also called cursor pagination) works by using a
// server-provided cursor value from the current page's response to request
// the next page. This avoids the "offset drift" problem of offset-based
// pagination and is efficient on large datasets because the database can
// seek directly to the next keyset position. It is also what Bitbucket
// Server's paginated APIs use ("nextPageStart"/"isLastPage"), the motivating
// use case for this adapter.
//
// Typical flow:
//  1. Client fetches page 1 with no cursor (or a configured start value).
//  2. Server returns results and a cursor field (CursorPath) in the response,
//     plus (optionally) an explicit "is this the last page" boolean
//     (LastPagePath).
//  3. Client sends the cursor value as a query parameter or path segment
//     (CursorParam) to fetch the next page.
//  4. Repeat until LastPagePath reports true, or — when LastPagePath is not
//     configured — until CursorPath is absent/empty in the response,
//     indicating there is no further page.
//
// Because each page's URL depends on the previous page's response, cursor
// pagination is inherently sequential: Cursor implements SequentialAdapter
// rather than relying on the counted Evaluate+BuildURL(pageNum) protocol used
// by PageNumber/Offset. Evaluate always errors — callers must type-assert to
// SequentialAdapter and drive pagination via BuildURL (start page only) and
// NextPageURL (every subsequent page), walking one page at a time; this
// cannot be parallelized.
type Cursor struct {
	cfg config.PaginationConfig
}

func newCursor(cfg config.PaginationConfig) *Cursor {
	return &Cursor{cfg: cfg}
}

// Evaluate is not supported for cursor-based pagination: there is no way to
// know the total page/entry count ahead of time, since each page's cursor is
// only revealed by the previous page's response. Callers must type-assert the
// adapter to SequentialAdapter and use BuildURL/NextPageURL instead of the
// counted Evaluate+BuildURL(pageNum) protocol.
func (c *Cursor) Evaluate(_ map[string]any) (*PageInfo, error) {
	return nil, fmt.Errorf(
		"pagination: cursor-based pagination does not support Evaluate; " +
			"use the SequentialAdapter interface (BuildURL for the start page, then NextPageURL per page)")
}

// BuildURL constructs the request URL for the start page only. Cursor
// pagination has no notion of an arbitrary pageNum — every page after the
// first is reached via NextPageURL, driven by the previous page's response —
// so BuildURL returns an error for any pageNum other than cfg.StartPage.
//
// injection: path — replaces {limit_param} in the URL path. The cursor
// placeholder ({cursor_param}) is left for the caller to omit entirely on the
// start page by simply not including it in the URL template, or is removed
// if present with an empty value, since there is no cursor value yet:
//
//	baseURL = "https://api.example.com/v1/items/{limit}"
//	result  = "https://api.example.com/v1/items/1000"
//
// injection: query_params — appends/overwrites limit_param, and removes any
// pre-existing cursor_param query value (there is no cursor yet on the start
// page):
//
//	baseURL = "https://api.example.com/v1/items?filter=active"
//	result  = "https://api.example.com/v1/items?filter=active&limit=1000"
func (c *Cursor) BuildURL(baseURL string, pageNum int) (string, error) {
	if pageNum != c.cfg.StartPage {
		return "", fmt.Errorf(
			"pagination: cursor-based pagination cannot build an arbitrary page %d directly; "+
				"use NextPageURL to advance sequentially from the start page (%d)", pageNum, c.cfg.StartPage)
	}

	switch c.cfg.Injection {
	case "path":
		u := strings.ReplaceAll(baseURL, "{"+c.cfg.LimitParam+"}", strconv.Itoa(c.cfg.PageSize))
		u = strings.ReplaceAll(u, "{"+c.cfg.CursorParam+"}", "")
		return u, nil
	case "query_params":
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("pagination: parse base URL: %w", err)
		}
		q := u.Query()
		q.Set(c.cfg.LimitParam, strconv.Itoa(c.cfg.PageSize))
		q.Del(c.cfg.CursorParam)
		u.RawQuery = q.Encode()
		return u.String(), nil
	case "body":
		return "", fmt.Errorf("pagination: body injection is not implemented for cursor-based pagination")
	default:
		return "", fmt.Errorf("pagination: unknown injection type %q", c.cfg.Injection)
	}
}

// NextPageURL inspects currentPageResponse (the top-level JSON object of the
// page that was just fetched) and returns the URL for the next page.
//
// Termination is determined as follows:
//   - If cfg.LastPagePath is set, it is read as a bool (jsonpath.GetBool);
//     true means there is no next page.
//   - Otherwise, termination is signalled by the absence of a usable cursor
//     value at cfg.CursorPath: if the path is missing, or resolves to an
//     empty string, pagination is considered complete.
//
// When there is a next page, the cursor value (numeric or string) is read
// from cfg.CursorPath and injected into baseURL as cfg.CursorParam, alongside
// cfg.LimitParam, using the configured Injection mode.
func (c *Cursor) NextPageURL(baseURL string, currentPageResponse map[string]any) (string, bool, error) {
	if c.cfg.LastPagePath != "" {
		isLast, err := jsonpath.GetBool(currentPageResponse, c.cfg.LastPagePath)
		if err != nil {
			return "", false, fmt.Errorf("pagination: read last_page_path %q: %w", c.cfg.LastPagePath, err)
		}
		if isLast {
			return "", false, nil
		}
	}

	cursorVal, err := jsonpath.Get(currentPageResponse, c.cfg.CursorPath)
	if err != nil {
		// No cursor value present: when last_page_path isn't configured,
		// this is the termination signal rather than an error.
		if c.cfg.LastPagePath == "" {
			return "", false, nil
		}
		return "", false, fmt.Errorf("pagination: read cursor_path %q: %w", c.cfg.CursorPath, err)
	}

	cursorStr, err := cursorToString(cursorVal)
	if err != nil {
		return "", false, fmt.Errorf("pagination: cursor_path %q: %w", c.cfg.CursorPath, err)
	}
	if cursorStr == "" {
		// Explicit empty/null cursor value also signals no next page when
		// no separate last_page_path is configured to say otherwise.
		if c.cfg.LastPagePath == "" {
			return "", false, nil
		}
	}

	switch c.cfg.Injection {
	case "path":
		u := strings.ReplaceAll(baseURL, "{"+c.cfg.CursorParam+"}", cursorStr)
		u = strings.ReplaceAll(u, "{"+c.cfg.LimitParam+"}", strconv.Itoa(c.cfg.PageSize))
		return u, true, nil
	case "query_params":
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", false, fmt.Errorf("pagination: parse base URL: %w", err)
		}
		q := u.Query()
		q.Set(c.cfg.CursorParam, cursorStr)
		q.Set(c.cfg.LimitParam, strconv.Itoa(c.cfg.PageSize))
		u.RawQuery = q.Encode()
		return u.String(), true, nil
	case "body":
		return "", false, fmt.Errorf("pagination: body injection is not implemented for cursor-based pagination")
	default:
		return "", false, fmt.Errorf("pagination: unknown injection type %q", c.cfg.Injection)
	}
}

// cursorToString stringifies a cursor value decoded from JSON, supporting
// both numeric cursors (e.g. Bitbucket's integer "nextPageStart") and opaque
// string/token cursors. nil (missing/null) stringifies to "".
func cursorToString(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case float64:
		// encoding/json decodes all JSON numbers as float64; cursor values
		// are integral page-start offsets in every known use case, so format
		// without a fractional component when the value is a whole number.
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case bool:
		return strconv.FormatBool(val), nil
	default:
		return "", fmt.Errorf("unsupported cursor value type %T", v)
	}
}
