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
