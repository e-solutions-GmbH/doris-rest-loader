// Package flattener transforms entity maps before they are streamed to Doris.
//
// Default behaviour (enabled: true, no include list, max_depth: 0):
//
//	{"user": {"id": 1, "name": "alice"}, "score": 99}
//	→ {"user.id": 1, "user.name": "alice", "score": 99}
//
// Depth-limited mode (max_depth: N > 0):
//
//	Flattening stops at N levels. Sub-objects at that depth are kept as
//	map[string]any leaf values; the schema inferrer maps them to Doris JSON
//	columns and json.Encoder serialises them as JSON literals on the wire.
//
//	With max_depth: 1:
//	  {"project": {"id": 1, "meta": {"color": "red"}}}
//	  → {"project.id": 1, "project.meta": {"color": "red"}}
//	  schema: project.id DOUBLE, project.meta JSON
//
// Field-selection mode (include list configured):
//
//	Only the listed fields are emitted. Each selected nested value is still
//	flattened if enabled is true (subject to max_depth).
//
// Pass-through mode (enabled: false, no include list):
//
//	The entity is returned as-is without any transformation.
package flattener

import (
	"fmt"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// Flattener transforms entity maps according to the configured strategy.
type Flattener struct {
	enabled    bool
	separator  string
	maxDepth   int // 0 = unlimited
	include    []config.FieldMapping
	rawJSONCol string // when non-empty, inject full raw entity as JSON string under this column name
}

// New creates a Flattener from the given configuration.
// Defaults applied by config.Load ensure that Enabled is non-nil and
// Separator is set before this constructor is called.
func New(cfg config.FlatteningConfig) *Flattener {
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	sep := cfg.Separator
	if sep == "" {
		sep = "."
	}
	return &Flattener{
		enabled:    enabled,
		separator:  sep,
		maxDepth:   cfg.MaxDepth,
		include:    cfg.Include,
		rawJSONCol: cfg.RawJSON,
	}
}

// Flatten processes a single entity map and returns the transformed result.
//
// Priority order:
//  1. If an include list is configured, only the listed fields are selected
//     (with optional column rename via the As field). Each selected value is
//     recursively flattened if enabled is true.
//  2. If only flattening is enabled (no include list), the entire entity is
//     recursively flattened into dot-notation keys.
//  3. If neither applies, the entity is returned unchanged.
//
// In all cases, if rawJSONCol is configured the full original entity is
// stored as map[string]any under that column name so the schema inferrer
// types it as a Doris JSON column (not VARCHAR).
func (f *Flattener) Flatten(entity map[string]any) (map[string]any, error) {
	var result map[string]any
	var err error

	if len(f.include) > 0 {
		result, err = f.selectFields(entity)
	} else if f.enabled {
		result = make(map[string]any, len(entity))
		flattenMap(entity, "", f.separator, f.maxDepth, 0, result)
	} else {
		// pass-through — copy to avoid mutating the original
		result = make(map[string]any, len(entity))
		for k, v := range entity {
			result[k] = v
		}
	}

	if err != nil {
		return nil, err
	}

	if f.rawJSONCol != "" {
		// Store the original entity map directly so the schema inferrer
		// types this column as Doris JSON (not VARCHAR).
		result[f.rawJSONCol] = entity
	}

	return result, nil
}

// FlattenAll applies Flatten to every entity in the slice and returns a new slice.
func (f *Flattener) FlattenAll(entities []map[string]any) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(entities))
	for i, entity := range entities {
		flat, err := f.Flatten(entity)
		if err != nil {
			return nil, fmt.Errorf("flattener: entity index %d: %w", i, err)
		}
		out = append(out, flat)
	}
	return out, nil
}

// selectFields builds an output map containing only the configured include fields.
// For each field:
//   - The value is looked up via its dot-notation Path.
//   - If the value is a nested object and flattening is enabled, it is recursively
//     flattened using the configured separator, prefixed by the output column name.
//   - The output key is the As name when set, otherwise the original Path.
//   - Fields that are absent from the entity are silently skipped.
func (f *Flattener) selectFields(entity map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(f.include))
	for _, field := range f.include {
		val, err := jsonpath.Get(entity, field.Path)
		if err != nil {
			// Field absent – skip silently rather than failing the whole entity.
			continue
		}
		outputKey := field.As
		if outputKey == "" {
			outputKey = field.Path
		}
		// If the selected value is itself a nested object and flattening is on,
		// flatten it into the result map prefixed with the output key.
		if f.enabled {
			if nested, ok := val.(map[string]any); ok {
				flattenMap(nested, outputKey, f.separator, f.maxDepth, 0, result)
				continue
			}
		}
		result[outputKey] = val
	}
	return result, nil
}

// flattenMap recursively walks data and writes each leaf value into result.
// Keys are built by joining ancestor key names with sep.
// Arrays are treated as leaf values and written as-is (Doris supports ARRAY types).
// Nested objects are treated as leaf values when maxDepth > 0 and currentDepth >= maxDepth-1;
// the caller receives a map[string]any which the schema inferrer maps to a Doris JSON column.
func flattenMap(data map[string]any, prefix, sep string, maxDepth, currentDepth int, result map[string]any) {
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + sep + k
		}
		if nested, ok := v.(map[string]any); ok && (maxDepth == 0 || currentDepth < maxDepth-1) {
			flattenMap(nested, key, sep, maxDepth, currentDepth+1, result)
		} else {
			result[key] = v
		}
	}
}
