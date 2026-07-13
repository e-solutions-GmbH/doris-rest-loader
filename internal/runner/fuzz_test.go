// fuzz_test.go stress-tests the two most parsing-heavy seams of the runner with
// Go's native fuzzing engine (go test -fuzz). Both fuzz targets assert a single
// robustness invariant: no matter how malformed the input JSON is, the code must
// return an error rather than panic. Malformed upstream API payloads are exactly
// the kind of input a REST loader faces in production, so a panic here would
// crash an ingestion worker.
package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// FuzzExtractEntities feeds arbitrary bytes (interpreted as JSON) and an
// arbitrary data_path into extractEntities. The contract under test: it must
// never panic — every unexpected shape has to surface as a returned error.
func FuzzExtractEntities(f *testing.F) {
	// Seed corpus: valid and pathological shapes.
	f.Add([]byte(`{"items":[{"id":1}]}`), "items")
	f.Add([]byte(`[{"id":1},{"id":2}]`), "")
	f.Add([]byte(`{"data":{"items":[{"id":1}]}}`), "data.items")
	f.Add([]byte(`{"id":1}`), "")
	f.Add([]byte(`{"items":"not-an-array"}`), "items")
	f.Add([]byte(`[1,2,3]`), "")
	f.Add([]byte(`null`), "missing.path")
	f.Add([]byte(`"just a string"`), "a.b.c")
	f.Add([]byte(`{}`), "")
	f.Add([]byte(``), "items")

	f.Fuzz(func(t *testing.T, body []byte, dataPath string) {
		var raw any
		if err := json.Unmarshal(body, &raw); err != nil {
			// Invalid JSON is out of scope for extractEntities; the fetcher
			// rejects it upstream. Skip so the corpus focuses on decoded values.
			t.Skip()
		}
		// The only invariant: extractEntities must not panic on any decoded shape.
		entities, err := extractEntities(raw, dataPath)
		if err == nil {
			// On success every element must be a non-nil object map.
			for i, e := range entities {
				if e == nil {
					t.Fatalf("entity %d is nil despite a nil error", i)
				}
			}
		}
	})
}

// FuzzRunnerPipeline runs the entire ingestion pipeline against a mock source
// that returns fuzz-controlled bytes, with the Doris sink always succeeding.
// The invariant: an arbitrary response body must either ingest cleanly or
// return an error — never panic, never hang.
func FuzzRunnerPipeline(f *testing.F) {
	f.Add([]byte(`{"items":[{"id":1}],"meta":{"totalPages":1}}`))
	f.Add([]byte(`{"items":[],"meta":{"totalPages":1}}`))
	f.Add([]byte(`{"items":[{"id":1,"nested":{"x":2}}],"meta":{"totalPages":1}}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{"items":"wrong-type"}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, body []byte) {
		// Doris sink: always succeeds, drains the request body.
		dorisSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Status":"Success","NumberLoadedRows":1}`))
		}))
		defer dorisSrv.Close()

		// Source: replies with the fuzzed bytes for every page request.
		sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}))
		defer sourceSrv.Close()

		cfg := baseConfig(sourceSrv.URL, dorisSrv.URL)
		cfg.Source.DataPath = "items"
		cfg.Pagination = config.PaginationConfig{
			Type: "page_number", Injection: "query_params", StartPage: 1,
			PageParam: "page", LimitParam: "limit", PageSize: 10, NumPagesPath: "meta.totalPages",
		}

		r, err := New(cfg)
		if err != nil {
			return // invalid config combinations are not the target here
		}

		// Bound each iteration so a pathological body can never hang the fuzzer.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Contract: Run returns (possibly an error) without panicking.
		_ = r.Run(ctx)
	})
}
