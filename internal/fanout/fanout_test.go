package fanout

import (
	"testing"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

func singleField(placeholder, path string) config.FieldSpec {
	return config.FieldSpec{{Placeholder: placeholder, Path: path}}
}

func TestExtractItems(t *testing.T) {
	tests := []struct {
		name     string
		entities []map[string]any
		fields   config.FieldSpec
		want     []Values
		wantErr  bool
	}{
		{
			name: "simple string field",
			entities: []map[string]any{
				{"key": "a"},
				{"key": "b"},
			},
			fields: singleField("key", "key"),
			want:   []Values{{"key": "a"}, {"key": "b"}},
		},
		{
			name: "deduplicates repeated values",
			entities: []map[string]any{
				{"key": "a"},
				{"key": "b"},
				{"key": "a"},
			},
			fields: singleField("key", "key"),
			want:   []Values{{"key": "a"}, {"key": "b"}},
		},
		{
			name: "dot-notation nested field",
			entities: []map[string]any{
				{"meta": map[string]any{"id": "x1"}},
				{"meta": map[string]any{"id": "x2"}},
			},
			fields: singleField("id", "meta.id"),
			want:   []Values{{"id": "x1"}, {"id": "x2"}},
		},
		{
			name: "non-string value stringified",
			entities: []map[string]any{
				{"id": float64(42)},
			},
			fields: singleField("id", "id"),
			want:   []Values{{"id": "42"}},
		},
		{
			name:     "empty entities list yields empty items",
			entities: []map[string]any{},
			fields:   singleField("key", "key"),
			want:     []Values{},
		},
		{
			name: "missing field errors",
			entities: []map[string]any{
				{"other": "a"},
			},
			fields:  singleField("key", "key"),
			wantErr: true,
		},
		{
			name: "multi-field dedup by combined tuple",
			entities: []map[string]any{
				{"project": map[string]any{"key": "AUDIO"}, "slug": "ath"},
				{"project": map[string]any{"key": "AUDIO"}, "slug": "ath"},
				{"project": map[string]any{"key": "AUDIO"}, "slug": "other"},
			},
			fields: config.FieldSpec{
				{Placeholder: "projectKey", Path: "project.key"},
				{Placeholder: "slug", Path: "slug"},
			},
			want: []Values{
				{"projectKey": "AUDIO", "slug": "ath"},
				{"projectKey": "AUDIO", "slug": "other"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractItems(tt.entities, tt.fields)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d items, got %d (%v)", len(tt.want), len(got), got)
			}
			for i := range got {
				for k, v := range tt.want[i] {
					if got[i][k] != v {
						t.Errorf("item %d: expected %s=%q, got %q", i, k, v, got[i][k])
					}
				}
			}
		})
	}
}

func TestExtractItems_NoDuplicatesInOutput(t *testing.T) {
	entities := []map[string]any{
		{"key": "a"}, {"key": "a"}, {"key": "a"}, {"key": "b"},
	}
	got, err := ExtractItems(entities, singleField("key", "key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduplicated items, got %d (%v)", len(got), got)
	}
}

func TestInjectItemColumns(t *testing.T) {
	t.Run("injects every value as its own column into every entity", func(t *testing.T) {
		entities := []map[string]any{
			{"metric": "branch_coverage"},
			{"metric": "conditions_to_cover"},
		}
		InjectItemColumns(entities, Values{"project_key": "my-project", "slug": "repo-a"})

		for i, e := range entities {
			if got := e["project_key"]; got != "my-project" {
				t.Errorf("entity %d: expected project_key=%q, got %v", i, "my-project", got)
			}
			if got := e["slug"]; got != "repo-a" {
				t.Errorf("entity %d: expected slug=%q, got %v", i, "repo-a", got)
			}
		}
	})

	t.Run("overwrites pre-existing key with same name", func(t *testing.T) {
		entities := []map[string]any{
			{"project_key": "stale-value", "metric": "x"},
		}
		InjectItemColumns(entities, Values{"project_key": "fresh-value"})

		if got := entities[0]["project_key"]; got != "fresh-value" {
			t.Errorf("expected project_key to be overwritten to %q, got %v", "fresh-value", got)
		}
	})

	t.Run("empty values is a no-op", func(t *testing.T) {
		entities := []map[string]any{
			{"metric": "branch_coverage"},
		}
		InjectItemColumns(entities, Values{})

		if len(entities[0]) != 1 {
			t.Errorf("expected entity unmodified, got %v", entities[0])
		}
	})
}

func TestBuildItemURL(t *testing.T) {
	tests := []struct {
		name        string
		urlTemplate string
		values      Values
		want        string
	}{
		{
			name:        "single occurrence",
			urlTemplate: "https://api.example.com/v1/items/{item}/details",
			values:      Values{"item": "abc123"},
			want:        "https://api.example.com/v1/items/abc123/details",
		},
		{
			name:        "custom placeholder name",
			urlTemplate: "https://api.example.com/v1/items/{key}/details",
			values:      Values{"key": "abc123"},
			want:        "https://api.example.com/v1/items/abc123/details",
		},
		{
			name:        "multiple occurrences of one placeholder all substituted",
			urlTemplate: "https://api.example.com/{item}/sub/{item}",
			values:      Values{"item": "x"},
			want:        "https://api.example.com/x/sub/x",
		},
		{
			name:        "no placeholder present leaves URL unchanged",
			urlTemplate: "https://api.example.com/v1/items/details",
			values:      Values{"item": "abc123"},
			want:        "https://api.example.com/v1/items/details",
		},
		{
			name:        "multiple distinct placeholders substituted",
			urlTemplate: "https://api.example.com/projects/{projectKey}/repos/{slug}",
			values:      Values{"projectKey": "AUDIO", "slug": "ath"},
			want:        "https://api.example.com/projects/AUDIO/repos/ath",
		},
		{
			name:        "combines with other non-item placeholders left intact",
			urlTemplate: "https://api.example.com/items/{item}/details/{page}/{limit}",
			values:      Values{"item": "abc123"},
			want:        "https://api.example.com/items/abc123/details/{page}/{limit}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildItemURL(tt.urlTemplate, tt.values)
			if got != tt.want {
				t.Errorf("BuildItemURL(%q, %v) = %q, want %q",
					tt.urlTemplate, tt.values, got, tt.want)
			}
		})
	}
}

func TestExtractFirstLast(t *testing.T) {
	firstFields := singleField("head", "id")
	lastFields := singleField("root", "id")

	t.Run("single page: first and last from same page", func(t *testing.T) {
		pages := [][]map[string]any{
			{
				{"id": "sha-head", "parents": []any{"sha-mid"}},
				{"id": "sha-mid", "parents": []any{"sha-root"}},
				{"id": "sha-root", "parents": []any{}},
			},
		}
		got, err := ExtractFirstLast(pages, firstFields, lastFields)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["head"] != "sha-head" {
			t.Errorf("expected head=sha-head, got %q", got["head"])
		}
		if got["root"] != "sha-root" {
			t.Errorf("expected root=sha-root, got %q", got["root"])
		}
	})

	t.Run("multi page: first of first page, last of last page", func(t *testing.T) {
		pages := [][]map[string]any{
			{{"id": "sha-head"}, {"id": "sha-2"}},
			{{"id": "sha-3"}, {"id": "sha-root"}},
		}
		got, err := ExtractFirstLast(pages, firstFields, lastFields)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["head"] != "sha-head" {
			t.Errorf("expected head=sha-head, got %q", got["head"])
		}
		if got["root"] != "sha-root" {
			t.Errorf("expected root=sha-root, got %q", got["root"])
		}
	})

	t.Run("no pages is a hard error", func(t *testing.T) {
		_, err := ExtractFirstLast(nil, firstFields, lastFields)
		if err == nil {
			t.Fatal("expected error for empty pages slice")
		}
	})

	t.Run("empty first page is a hard error", func(t *testing.T) {
		pages := [][]map[string]any{{}, {{"id": "sha-root"}}}
		_, err := ExtractFirstLast(pages, firstFields, lastFields)
		if err == nil {
			t.Fatal("expected error for empty first page")
		}
	})

	t.Run("empty last page is a hard error", func(t *testing.T) {
		pages := [][]map[string]any{{{"id": "sha-head"}}, {}}
		_, err := ExtractFirstLast(pages, firstFields, lastFields)
		if err == nil {
			t.Fatal("expected error for empty last page")
		}
	})

	t.Run("missing field errors", func(t *testing.T) {
		pages := [][]map[string]any{{{"other": "x"}}}
		_, err := ExtractFirstLast(pages, firstFields, lastFields)
		if err == nil {
			t.Fatal("expected error for missing field")
		}
	})
}

func TestValues_Label(t *testing.T) {
	tests := []struct {
		name string
		v    Values
		want string
	}{
		{name: "empty", v: Values{}, want: "(no fields)"},
		{name: "single", v: Values{"key": "a"}, want: "key=a"},
		{
			name: "multiple sorted deterministically",
			v:    Values{"slug": "ath", "projectKey": "AUDIO"},
			want: "projectKey=AUDIO, slug=ath",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.Label(); got != tt.want {
				t.Errorf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValues_Merge(t *testing.T) {
	a := Values{"x": "1", "y": "2"}
	b := Values{"y": "3", "z": "4"}
	got := a.Merge(b)

	want := Values{"x": "1", "y": "3", "z": "4"}
	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %d (%v)", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Merge()[%q] = %q, want %q", k, got[k], v)
		}
	}
	// Originals must be unmodified.
	if a["y"] != "2" {
		t.Errorf("Merge mutated receiver: a[%q] = %q, want %q", "y", a["y"], "2")
	}
}
