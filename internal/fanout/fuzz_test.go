// fuzz_test.go stress-tests fan-out placeholder substitution with Go's native
// fuzzing engine (go test -fuzz). URL templates come from user configuration,
// so BuildItemURL must handle arbitrary strings without panicking and must
// always substitute every occurrence of the placeholder.
package fanout

import (
	"strings"
	"testing"
)

// FuzzBuildItemURL exercises BuildItemURL with fuzz-controlled URL templates,
// placeholder names, and item values. Contract: BuildItemURL never panics,
// and when the placeholder occurs in the template, the result must contain
// no further occurrences of "{placeholder}" (every occurrence was replaced)
// unless the item value itself reintroduces the literal placeholder text.
func FuzzBuildItemURL(f *testing.F) {
	f.Add("https://api.example.com/v1/items/{item}/details", "item", "abc123")
	f.Add("https://api.example.com/v1/items/{item}/details/{page}/{limit}", "item", "x")
	f.Add("no-placeholder-here", "item", "x")
	f.Add("", "item", "")
	f.Add("{item}{item}{item}", "item", "y")
	f.Add("https://api.example.com/{key}", "key", "../../etc/passwd")

	f.Fuzz(func(t *testing.T, urlTemplate, placeholder, item string) {
		if placeholder == "" {
			// Empty placeholder name produces the "{}" token; still must not panic.
			_ = BuildItemURL(urlTemplate, placeholder, item)
			return
		}

		out := BuildItemURL(urlTemplate, placeholder, item)

		token := "{" + placeholder + "}"
		// If the item value doesn't itself reintroduce the placeholder token,
		// every occurrence of the token in the template must have been replaced.
		if !strings.Contains(item, token) && strings.Contains(out, token) {
			t.Fatalf("BuildItemURL(%q, %q, %q) = %q still contains unsubstituted placeholder %q",
				urlTemplate, placeholder, item, out, token)
		}
	})
}
