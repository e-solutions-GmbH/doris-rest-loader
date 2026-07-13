// fuzz_test.go stress-tests the flattener with Go's native fuzzing engine
// (go test -fuzz). Entities arrive from arbitrary upstream REST APIs, so the
// flattener must tolerate any decoded JSON object — arbitrarily deep nesting,
// mixed value types, empty keys — without panicking. Each target asserts that
// invariant.
package flattener

import (
	"encoding/json"
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// FuzzFlatten feeds arbitrary JSON objects into Flatten under a range of
// separators and depth limits. Contract: no panic, and on success the result
// must contain no nested map values below the configured depth is not asserted
// here (covered by unit tests) — the fuzz target focuses purely on robustness.
func FuzzFlatten(f *testing.F) {
	f.Add([]byte(`{"id":1,"name":"alice"}`), ".", 0)
	f.Add([]byte(`{"user":{"id":1,"name":"bob"}}`), ".", 0)
	f.Add([]byte(`{"a":{"b":{"c":{"d":1}}}}`), ".", 2)
	f.Add([]byte(`{"tags":[1,2,3]}`), "_", 0)
	f.Add([]byte(`{"":{"":1}}`), ".", 0)
	f.Add([]byte(`{"k":null}`), ".", 1)
	f.Add([]byte(`{}`), ".", 0)

	f.Fuzz(func(t *testing.T, body []byte, sep string, maxDepth int) {
		var entity map[string]any
		if err := json.Unmarshal(body, &entity); err != nil || entity == nil {
			// Only well-formed JSON objects reach the flattener in production.
			t.Skip()
		}
		// Clamp maxDepth to a sane range so the fuzzer explores meaningful
		// limits instead of huge/negative integers (negative behaves like 0).
		if maxDepth < 0 {
			maxDepth = 0
		}
		if maxDepth > 32 {
			maxDepth = 32
		}
		if sep == "" {
			sep = "."
		}

		enabled := true
		fl := New(config.FlatteningConfig{Enabled: &enabled, Separator: sep, MaxDepth: maxDepth})

		// Invariant: Flatten must not panic on any decoded object.
		result, err := fl.Flatten(entity)
		if err == nil && result == nil {
			t.Fatal("Flatten returned nil map with nil error")
		}
	})
}
