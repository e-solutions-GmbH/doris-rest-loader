package fanout

import "testing"

func TestExtractItems(t *testing.T) {
	tests := []struct {
		name      string
		entities  []map[string]any
		itemField string
		want      []string
		wantErr   bool
	}{
		{
			name: "simple string field",
			entities: []map[string]any{
				{"key": "a"},
				{"key": "b"},
			},
			itemField: "key",
			want:      []string{"a", "b"},
		},
		{
			name: "deduplicates repeated values",
			entities: []map[string]any{
				{"key": "a"},
				{"key": "b"},
				{"key": "a"},
			},
			itemField: "key",
			want:      []string{"a", "b"},
		},
		{
			name: "dot-notation nested field",
			entities: []map[string]any{
				{"meta": map[string]any{"id": "x1"}},
				{"meta": map[string]any{"id": "x2"}},
			},
			itemField: "meta.id",
			want:      []string{"x1", "x2"},
		},
		{
			name: "non-string value stringified",
			entities: []map[string]any{
				{"id": float64(42)},
			},
			itemField: "id",
			want:      []string{"42"},
		},
		{
			name:      "empty entities list yields empty items",
			entities:  []map[string]any{},
			itemField: "key",
			want:      []string{},
		},
		{
			name: "missing field errors",
			entities: []map[string]any{
				{"other": "a"},
			},
			itemField: "key",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractItems(tt.entities, tt.itemField)
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
			wantSet := make(map[string]bool, len(tt.want))
			for _, w := range tt.want {
				wantSet[w] = true
			}
			for _, g := range got {
				if !wantSet[g] {
					t.Errorf("unexpected item %q in result %v", g, got)
				}
			}
		})
	}
}

func TestExtractItems_NoDuplicatesInOutput(t *testing.T) {
	entities := []map[string]any{
		{"key": "a"}, {"key": "a"}, {"key": "a"}, {"key": "b"},
	}
	got, err := ExtractItems(entities, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduplicated items, got %d (%v)", len(got), got)
	}
}

func TestInjectItemColumn(t *testing.T) {
	t.Run("injects column into every entity", func(t *testing.T) {
		entities := []map[string]any{
			{"metric": "branch_coverage"},
			{"metric": "conditions_to_cover"},
		}
		InjectItemColumn(entities, "project_key", "my-project")

		for i, e := range entities {
			if got := e["project_key"]; got != "my-project" {
				t.Errorf("entity %d: expected project_key=%q, got %v", i, "my-project", got)
			}
		}
	})

	t.Run("overwrites pre-existing key with same name", func(t *testing.T) {
		entities := []map[string]any{
			{"project_key": "stale-value", "metric": "x"},
		}
		InjectItemColumn(entities, "project_key", "fresh-value")

		if got := entities[0]["project_key"]; got != "fresh-value" {
			t.Errorf("expected project_key to be overwritten to %q, got %v", "fresh-value", got)
		}
	})

	t.Run("empty column is a no-op", func(t *testing.T) {
		entities := []map[string]any{
			{"metric": "branch_coverage"},
		}
		InjectItemColumn(entities, "", "my-project")

		if _, ok := entities[0][""]; ok {
			t.Errorf("expected no key injected when column is empty, got %v", entities[0])
		}
		if len(entities[0]) != 1 {
			t.Errorf("expected entity unmodified, got %v", entities[0])
		}
	})
}

func TestBuildItemURL(t *testing.T) {
	tests := []struct {
		name        string
		urlTemplate string
		placeholder string
		item        string
		want        string
	}{
		{
			name:        "single occurrence",
			urlTemplate: "https://api.example.com/v1/items/{item}/details",
			placeholder: "item",
			item:        "abc123",
			want:        "https://api.example.com/v1/items/abc123/details",
		},
		{
			name:        "custom placeholder name",
			urlTemplate: "https://api.example.com/v1/items/{key}/details",
			placeholder: "key",
			item:        "abc123",
			want:        "https://api.example.com/v1/items/abc123/details",
		},
		{
			name:        "multiple occurrences all substituted",
			urlTemplate: "https://api.example.com/{item}/sub/{item}",
			placeholder: "item",
			item:        "x",
			want:        "https://api.example.com/x/sub/x",
		},
		{
			name:        "no placeholder present leaves URL unchanged",
			urlTemplate: "https://api.example.com/v1/items/details",
			placeholder: "item",
			item:        "abc123",
			want:        "https://api.example.com/v1/items/details",
		},
		{
			name:        "combines with other placeholders left intact",
			urlTemplate: "https://api.example.com/items/{item}/details/{page}/{limit}",
			placeholder: "item",
			item:        "abc123",
			want:        "https://api.example.com/items/abc123/details/{page}/{limit}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildItemURL(tt.urlTemplate, tt.placeholder, tt.item)
			if got != tt.want {
				t.Errorf("BuildItemURL(%q, %q, %q) = %q, want %q",
					tt.urlTemplate, tt.placeholder, tt.item, got, tt.want)
			}
		})
	}
}
