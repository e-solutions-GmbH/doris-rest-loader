// Package fanout implements item-extraction and URL-substitution for the
// "fan-out" source mode: list parent entities/identifiers, then issue one
// parameterised detail request per identifier.
package fanout

import (
	"fmt"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/jsonpath"
)

// ExtractItems extracts the dot-notation itemField value from each list
// entity and returns a deduplicated list of fan-out item values.
func ExtractItems(entities []map[string]any, itemField string) ([]string, error) {
	seen := make(map[string]struct{}, len(entities))
	items := make([]string, 0, len(entities))

	for i, entity := range entities {
		val, err := jsonpath.GetString(entity, itemField)
		if err != nil {
			return nil, fmt.Errorf("extract item_field %q from list entity %d: %w", itemField, i, err)
		}
		if _, dup := seen[val]; dup {
			continue
		}
		seen[val] = struct{}{}
		items = append(items, val)
	}

	return items, nil
}

// BuildItemURL substitutes item into every occurrence of {placeholder} within
// urlTemplate.
func BuildItemURL(urlTemplate, placeholder, item string) string {
	return strings.ReplaceAll(urlTemplate, "{"+placeholder+"}", item)
}

// InjectItemColumn sets entity[column] = item on every entity in entities,
// in place. This makes the fan-out item value (otherwise only used to build
// the per-item request URL) available as an ordinary flattened column on
// every entity extracted from that item's detail response — needed when the
// detail endpoint's response body doesn't itself echo back the identifier
// used to request it.
//
// If an entity already has a key matching column, the fan-out item value
// overwrites it, mirroring flattening.raw_json_column's existing overwrite
// semantics. A no-op when column is empty (item_column unset).
func InjectItemColumn(entities []map[string]any, column, item string) {
	if column == "" {
		return
	}
	for _, entity := range entities {
		entity[column] = item
	}
}
