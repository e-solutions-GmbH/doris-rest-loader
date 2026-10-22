// Package fanout implements item-extraction and URL-substitution for the
// "fan-out" source mode: list parent entities/identifiers, then issue one
// parameterised detail request per identifier.
//
// BREAKING CHANGE: the former single-field ExtractItems/BuildItemURL/
// InjectItemColumn API (keyed on a single item_field/item_placeholder/
// item_column string) has been replaced by a map-based,
// config.FieldSpec-driven API supporting one or more named fields per
// fan-out item. There is no backward-compatible alias for the old API.
package fanout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// Values is one fan-out item's resolved field values: placeholder name ->
// extracted string value. The same shape is used for list-stage item
// identity (config.FanOutConfig.ItemFields) and for sub-list first/last
// extraction results (config.SubListConfig.FirstItemFields/LastItemFields) —
// both are merged into a single Values map per item.
type Values map[string]string

// Label renders values deterministically (sorted by placeholder name) as
// "key1=value1, key2=value2", for logging. Go map iteration order is
// randomized, so callers must not log a Values map directly if the output
// needs to be stable/reproducible across runs.
func (v Values) Label() string {
	if len(v) == 0 {
		return "(no fields)"
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v[k]))
	}
	return strings.Join(parts, ", ")
}

// Merge returns a new Values containing every entry of v plus every entry of
// other, with other's entries taking precedence on key collision. Neither v
// nor other is mutated.
func (v Values) Merge(other Values) Values {
	out := make(Values, len(v)+len(other))
	for k, val := range v {
		out[k] = val
	}
	for k, val := range other {
		out[k] = val
	}
	return out
}

// ExtractItems extracts, from each list entity, the values named by fields,
// and returns a deduplicated list of Values (one per distinct combination of
// field values across all fields, preserving first-seen order).
func ExtractItems(entities []map[string]any, fields config.FieldSpec) ([]Values, error) {
	seen := make(map[string]struct{}, len(entities))
	items := make([]Values, 0, len(entities))

	for i, entity := range entities {
		vals, err := extractValues(entity, fields)
		if err != nil {
			return nil, fmt.Errorf("extract item_fields from list entity %d: %w", i, err)
		}
		key := dedupKey(fields, vals)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, vals)
	}

	return items, nil
}

// extractValues resolves every entry of fields against entity, returning a
// Values map keyed by placeholder name.
func extractValues(entity map[string]any, fields config.FieldSpec) (Values, error) {
	vals := make(Values, len(fields))
	for _, f := range fields {
		s, err := jsonpath.GetString(entity, f.Path)
		if err != nil {
			return nil, fmt.Errorf("field %q (path %q): %w", f.Placeholder, f.Path, err)
		}
		vals[f.Placeholder] = s
	}
	return vals, nil
}

// dedupKey builds a stable dedup key from vals, iterating fields in their
// declared order (not Go map iteration order) so the key is deterministic
// regardless of how Values happens to be laid out in memory.
func dedupKey(fields config.FieldSpec, vals Values) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Placeholder+"="+vals[f.Placeholder])
	}
	return strings.Join(parts, "\x1f")
}

// BuildItemURL substitutes every entry of values into its corresponding
// "{placeholder}" occurrence(s) within urlTemplate.
func BuildItemURL(urlTemplate string, values Values) string {
	out := urlTemplate
	for placeholder, val := range values {
		out = strings.ReplaceAll(out, "{"+placeholder+"}", val)
	}
	return out
}

// InjectItemColumns sets entity[placeholder] = value on every entity in
// entities, in place, for every entry of values. This makes the fan-out
// item's (and, when configured, the sub-list's first/last) field values —
// otherwise only used to build the per-item request URL — available as
// ordinary flattened columns on every entity extracted from that item's
// detail response(s). Needed when the detail endpoint's response body
// doesn't itself echo back the identifier(s) used to request it.
//
// If an entity already has a key matching one of values' placeholders, the
// fan-out value overwrites it, mirroring flattening.raw_json_column's
// existing overwrite semantics. A no-op when values is empty.
func InjectItemColumns(entities []map[string]any, values Values) {
	if len(values) == 0 {
		return
	}
	for _, entity := range entities {
		for placeholder, val := range values {
			entity[placeholder] = val
		}
	}
}

// ExtractFirstLast extracts firstFields from the first entity of the first
// page, and lastFields from the last entity of the last page, of pages (the
// ordered slice of per-page entity slices returned by a sub-list fetch).
//
// An empty pages slice, or an empty first/last page, is a hard error: an
// empty sub-list result is treated as a per-item failure, consistent with
// every other item-level failure in the fan-out pipeline — there is no
// silent empty-placeholder fallback.
func ExtractFirstLast(pages [][]map[string]any, firstFields, lastFields config.FieldSpec) (Values, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("sub-list returned no pages")
	}

	firstPage := pages[0]
	if len(firstPage) == 0 {
		return nil, fmt.Errorf("sub-list's first page is empty")
	}
	firstVals, err := extractValues(firstPage[0], firstFields)
	if err != nil {
		return nil, fmt.Errorf("extract first_item_fields from sub-list first entity: %w", err)
	}

	lastPage := pages[len(pages)-1]
	if len(lastPage) == 0 {
		return nil, fmt.Errorf("sub-list's last page is empty")
	}
	lastVals, err := extractValues(lastPage[len(lastPage)-1], lastFields)
	if err != nil {
		return nil, fmt.Errorf("extract last_item_fields from sub-list last entity: %w", err)
	}

	return firstVals.Merge(lastVals), nil
}
