// Package config — FieldSpec: the polymorphic "single field or named field
// set" YAML value shared by source.fanout.item_fields and
// source.fanout.sub_list.first_item_fields / last_item_fields.
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// FieldSpecEntry names one dot-notation field extraction: Path is the
// dot-notation location within an entity (e.g. "project.key"), and
// Placeholder is both the "{placeholder}" substituted into URL templates
// (fanout.BuildItemURL) and the column name auto-injected into every entity
// extracted under that identifier (fanout.InjectItemColumns) — there is no
// separate rename step.
type FieldSpecEntry struct {
	Placeholder string
	Path        string
}

// FieldSpec is a polymorphic YAML value describing one or more named field
// extractions. Two YAML shapes are supported:
//
//	# shorthand — a single field; the placeholder/column name is the path's
//	# leaf segment (e.g. "project.key" -> placeholder "key").
//	item_fields: "slug"
//
//	# mapping — one or more fields, each with an explicit placeholder/column
//	# name. Declaration order is preserved (YAML document order, not Go map
//	# iteration order).
//	item_fields:
//	  projectKey: "project.key"
//	  slug: "slug"
//
// FieldSpec is reused verbatim by source.fanout.item_fields and by
// source.fanout.sub_list.first_item_fields / last_item_fields.
type FieldSpec []FieldSpecEntry

// IsEmpty reports whether no fields were configured.
func (fs FieldSpec) IsEmpty() bool {
	return len(fs) == 0
}

// UnmarshalYAML implements the scalar-or-ordered-mapping FieldSpec syntax
// described on the type.
func (fs *FieldSpec) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" {
			*fs = nil
			return nil
		}
		var path string
		if err := value.Decode(&path); err != nil {
			return fmt.Errorf("field spec: %w", err)
		}
		if path == "" {
			*fs = nil
			return nil
		}
		*fs = FieldSpec{{Placeholder: leafSegment(path), Path: path}}
		return nil

	case yaml.MappingNode:
		if len(value.Content)%2 != 0 {
			return fmt.Errorf("field spec: malformed mapping")
		}
		entries := make(FieldSpec, 0, len(value.Content)/2)
		for i := 0; i < len(value.Content); i += 2 {
			keyNode, valNode := value.Content[i], value.Content[i+1]
			var placeholder, path string
			if err := keyNode.Decode(&placeholder); err != nil {
				return fmt.Errorf("field spec: decode placeholder name: %w", err)
			}
			if err := valNode.Decode(&path); err != nil {
				return fmt.Errorf("field spec: decode path for placeholder %q: %w", placeholder, err)
			}
			entries = append(entries, FieldSpecEntry{Placeholder: placeholder, Path: path})
		}
		*fs = entries
		return nil

	default:
		return fmt.Errorf("field spec: expected a string or mapping, got %v", value.Kind)
	}
}

// leafSegment returns the final dot-separated component of a dot-notation
// path, e.g. "project.key" -> "key", "slug" -> "slug".
func leafSegment(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return path[idx+1:]
	}
	return path
}
