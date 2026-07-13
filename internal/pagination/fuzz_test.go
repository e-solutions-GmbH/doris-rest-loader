// fuzz_test.go stress-tests pagination URL construction with Go's native
// fuzzing engine (go test -fuzz). Base URLs come from user configuration and
// upstream redirects, so BuildURL must handle arbitrary strings and page
// numbers without panicking. When it succeeds under query_params injection the
// result must remain a parseable URL.
package pagination

import (
	"net/url"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// FuzzBuildURL exercises both the offset and page_number adapters across the
// path and query_params injection modes with fuzz-controlled base URLs and page
// numbers. Contract: BuildURL never panics; a nil error under query_params
// implies the returned string parses as a URL.
func FuzzBuildURL(f *testing.F) {
	f.Add("https://api.example.com/v1/items", 1)
	f.Add("https://api.example.com/search?jql=project=FOO", 3)
	f.Add("https://api.example.com/items/{offset}/{limit}", 2)
	f.Add("https://api.example.com/items?p=1&ps=50", 0)
	f.Add("://not-a-url", 1)
	f.Add("", -5)
	f.Add("http://%zz", 10)

	f.Fuzz(func(t *testing.T, baseURL string, pageNum int) {
		injections := []string{"path", "query_params"}
		types := []string{"offset", "page_number"}

		for _, typ := range types {
			for _, inj := range injections {
				cfg := config.PaginationConfig{
					Type:        typ,
					Injection:   inj,
					StartPage:   1,
					PageSize:    50,
					PageParam:   "p",
					LimitParam:  "ps",
					OffsetParam: "startAt",
				}
				adapter, err := NewAdapter(cfg)
				if err != nil {
					continue
				}

				// Invariant: BuildURL must not panic for any input.
				out, err := adapter.BuildURL(baseURL, pageNum)
				if err != nil {
					continue
				}
				// On success under query_params the output must stay parseable.
				if inj == "query_params" {
					if _, perr := url.Parse(out); perr != nil {
						t.Fatalf("BuildURL(%q, %d) produced unparseable URL %q: %v",
							baseURL, pageNum, out, perr)
					}
				}
			}
		}
	})
}
