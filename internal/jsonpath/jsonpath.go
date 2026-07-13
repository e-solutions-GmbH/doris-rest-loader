// Package jsonpath provides a minimal dot-notation JSON path resolver.
// It operates on the map[string]any values produced by encoding/json.Unmarshal.
//
// Supported syntax: simple dot-separated keys, e.g. "meta.pagination.totalPages".
// Array indexing is not supported. Paths are case-sensitive.
package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
)

// Get navigates to the value at the given dot-notation path within data.
//
// Examples:
//
//	Get(data, "meta.totalPages")  →  data["meta"]["totalPages"]
//	Get(data, "items")            →  data["items"]
//	Get(data, "")                 →  data (the root map itself)
func Get(data map[string]any, path string) (any, error) {
	if path == "" {
		return data, nil
	}

	head, tail, _ := strings.Cut(path, ".")
	val, ok := data[head]
	if !ok {
		return nil, fmt.Errorf("jsonpath: key %q not found", head)
	}

	if tail == "" {
		return val, nil
	}

	nested, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jsonpath: value at key %q is not an object (got %T)", head, val)
	}

	return Get(nested, tail)
}

// GetInt navigates to the value at path and converts it to int.
//
// Supports the following value types produced by encoding/json.Unmarshal:
//   - float64 (standard JSON number)
//   - int / int64 (from hand-constructed maps in tests)
//   - string (parsed with strconv.Atoi)
func GetInt(data map[string]any, path string) (int, error) {
	val, err := Get(data, path)
	if err != nil {
		return 0, err
	}

	switch v := val.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("jsonpath: value at %q is string %q, cannot convert to int: %w", path, v, err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("jsonpath: value at %q has type %T, cannot convert to int", path, val)
	}
}

// GetString navigates to the value at path and converts it to string.
// Non-string values are formatted with fmt.Sprintf("%v", …).
func GetString(data map[string]any, path string) (string, error) {
	val, err := Get(data, path)
	if err != nil {
		return "", err
	}
	if s, ok := val.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", val), nil
}
