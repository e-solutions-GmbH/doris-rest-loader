// Package schema infers an Apache Doris column schema from a slice of
// pre-flattened entity maps and renders a ready-to-use CREATE TABLE DDL.
//
// This is used by the dry-run mode to let users inspect the data structure
// that would be streamed to Doris before creating the target table.
//
// Type inference rules (widest-compatible Doris type wins across all rows):
//
//	JSON bool             → BOOLEAN
//	JSON float64          → DOUBLE
//	JSON string           → VARCHAR(65533)
//	JSON []any (scalars)  → ARRAY<BOOLEAN|DOUBLE|VARCHAR(65533)>
//	                        (element type determined by widening across elements)
//	JSON []any (objects)  → ARRAY<JSON>
//	JSON []any (empty)    → ARRAY<JSON>  (safest default)
//	JSON map[string]any   → JSON  (sub-objects kept as leaves by max_depth or disabled flattening)
//	JSON null             → deferred; widens to the type of the first non-null value,
//	                        or STRING if every value is null
//
// Type widening order (higher index wins):
//
//	BOOLEAN < DOUBLE < VARCHAR(65533) < ARRAY<*> < JSON < STRING
//
// STRING is used only as a last-resort fallback when two incompatible inferred
// types appear for the same column across different rows (e.g. a DOUBLE in one
// row and an ARRAY in another — which should not happen with well-formed data
// but is handled gracefully).
package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Column holds the inferred name and Doris SQL type for a single output column.
type Column struct {
	Name      string
	DorisType string
}

// dorisTypePriority defines a precedence order used when two rows disagree on
// a column's type. The type with the higher index wins (is "wider").
// Concrete ARRAY<T> types share the same bucket — any ARRAY wins over scalars.
// JSON is wider than any ARRAY because it can represent arbitrary structure.
// STRING is the last-resort fallback and wins over everything so that schema
// mismatches (e.g. DOUBLE in one row, ARRAY in another) degrade gracefully.
var dorisTypePriority = map[string]int{
	"BOOLEAN":        0,
	"DOUBLE":         1,
	"VARCHAR(65533)": 2,
	// ARRAY<T> types are all priority 3; exact key checked via isArrayType().
	"JSON":   4,
	"STRING": 5, // widest — last-resort fallback
}

// isArrayType reports whether t is any ARRAY<…> type string.
func isArrayType(t string) bool {
	return strings.HasPrefix(t, "ARRAY<")
}

// arrayTypePriority is the shared priority for all ARRAY<T> types.
const arrayTypePriority = 3

// Infer scans all flattened entities and returns the inferred column list,
// sorted alphabetically by name. Each column's type is the widest Doris type
// observed across all entity rows for that column key.
func Infer(entities []map[string]any) []Column {
	// colTypes maps column name → current widest inferred Doris type.
	// A missing entry means the column has only been seen with null values so far.
	colTypes := make(map[string]string)

	for _, entity := range entities {
		for key, val := range entity {
			// Doris column names are case-insensitive; normalise to lower-case
			// so that keys differing only by case (e.g. "OEM" and "oem") are
			// merged into one column rather than producing a duplicate that
			// Doris would reject or silently conflate.
			normKey := strings.ToLower(key)

			inferred := inferType(val)
			if inferred == "" {
				// null value — do not narrow an already-observed type
				if _, seen := colTypes[normKey]; !seen {
					// Mark as seen-but-unresolved with empty string sentinel.
					colTypes[normKey] = ""
				}
				continue
			}
			existing, seen := colTypes[normKey]
			if !seen || existing == "" {
				colTypes[normKey] = inferred
				continue
			}
			colTypes[normKey] = widerType(existing, inferred)
		}
	}

	columns := make([]Column, 0, len(colTypes))
	for name, dorisType := range colTypes {
		if dorisType == "" {
			dorisType = "STRING" // all-null column — safest fallback
		}
		columns = append(columns, Column{Name: name, DorisType: dorisType})
	}
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Name < columns[j].Name
	})
	return columns
}

// DDL renders a CREATE TABLE skeleton for the given table and column list.
// The result is printed as-is so the user can paste it into a Doris SQL client.
//
// Notes:
//   - A DUPLICATE KEY on the first column is used as a safe default. Users
//     should review and adjust the key type and key columns for their use case.
//   - No DISTRIBUTED BY clause is emitted — Doris 2.x supports random
//     bucketing by default, but users may want to add explicit bucketing.
//   - Doris does not support a database-prefixed table name in CREATE TABLE;
//     run USE <database> first or prefix manually after generation if needed.
func DDL(table string, columns []Column) string {
	if len(columns) == 0 {
		return fmt.Sprintf("-- No columns could be inferred for table `%s`\n", table)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE `%s`\n(\n", table))
	for i, col := range columns {
		comma := ","
		if i == len(columns)-1 {
			comma = ""
		}
		sb.WriteString(fmt.Sprintf("    `%s` %s NULL%s\n", col.Name, col.DorisType, comma))
	}
	sb.WriteString(")\n")
	sb.WriteString(fmt.Sprintf("DUPLICATE KEY(`%s`)\n", columns[0].Name))
	sb.WriteString("COMMENT 'generated by doris-rest-loader --dry-run'\n")
	sb.WriteString("PROPERTIES (\"replication_num\" = \"1\");")
	return sb.String()
}

// inferType maps a Go value (as produced by json.Unmarshal into any) to a
// Doris type string. Returns "" for JSON null (nil pointer).
func inferType(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case bool:
		return "BOOLEAN"
	case float64:
		return "DOUBLE"
	case string:
		return "VARCHAR(65533)"
	case []any:
		return inferArrayType(v)
	case map[string]any:
		// Sub-object kept as a leaf (flattening disabled or max_depth reached).
		return "JSON"
	default:
		return "STRING"
	}
}

// inferArrayType inspects the elements of a JSON array and returns the most
// specific Doris ARRAY<T> type that fits all of them.
//
//   - Empty array           → ARRAY<JSON>  (no element type to inspect)
//   - All booleans          → ARRAY<BOOLEAN>
//   - Booleans and numbers  → ARRAY<DOUBLE>
//   - Any string scalar     → ARRAY<VARCHAR(65533)>
//   - Any object element    → ARRAY<JSON>
//
// Null elements are skipped (they are valid in all ARRAY types).
func inferArrayType(elems []any) string {
	if len(elems) == 0 {
		return "ARRAY<JSON>"
	}
	elemType := "" // widest element type seen so far; "" = only nulls seen
	for _, e := range elems {
		t := inferType(e)
		if t == "" {
			continue // null element
		}
		// An object element means the array is heterogeneous enough to need JSON.
		if t == "JSON" || t == "STRING" || isArrayType(t) {
			return "ARRAY<JSON>"
		}
		if elemType == "" {
			elemType = t
		} else {
			elemType = widerType(elemType, t)
		}
	}
	if elemType == "" {
		return "ARRAY<JSON>" // all elements were null
	}
	return fmt.Sprintf("ARRAY<%s>", elemType)
}

// widerType returns whichever of a or b has higher precedence (is "wider").
// If either type is unknown it defaults to STRING.
// A scalar type (BOOLEAN/DOUBLE/VARCHAR) mixed with a complex type (ARRAY/JSON)
// indicates a cross-category schema mismatch and also falls back to STRING.
func widerType(a, b string) string {
	pa := typePriority(a)
	pb := typePriority(b)
	if pa < 0 || pb < 0 {
		return "STRING"
	}
	// Cross-category mismatch: one side is a plain scalar, the other is
	// structured (ARRAY or JSON). Degrade to STRING rather than silently
	// promoting a scalar column to ARRAY/JSON.
	if isScalarType(a) != isScalarType(b) {
		return "STRING"
	}
	if pb > pa {
		return b
	}
	return a
}

// isScalarType reports whether t is one of the plain scalar Doris types
// (BOOLEAN, DOUBLE, VARCHAR). STRING, ARRAY<*>, and JSON are not scalar.
func isScalarType(t string) bool {
	switch t {
	case "BOOLEAN", "DOUBLE", "VARCHAR(65533)":
		return true
	}
	return false
}

// typePriority returns the widening priority for a Doris type string.
// Returns -1 for unknown types so the caller can fall back to STRING.
func typePriority(t string) int {
	if p, ok := dorisTypePriority[t]; ok {
		return p
	}
	if isArrayType(t) {
		return arrayTypePriority
	}
	return -1
}
