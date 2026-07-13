// Package doris provides a client for the Apache Doris Stream Load HTTP API.
//
// Doris Stream Load ingests data via a single HTTP PUT request to:
//
//	http://<fe-host>:8030/api/<database>/<table>/_stream_load
//
// The Doris FE (Frontend) node typically issues an HTTP 307 redirect to a BE
// (Backend) node. This is handled transparently by setting GetBody on the
// request, which allows the standard Go HTTP client to replay the body after
// the redirect.
//
// Data format: newline-delimited JSON (NDJSON), one JSON object per line,
// using the Doris "read_json_by_line" option.
package doris

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/e-solutions-GmbH/doris-rest-loader/internal/config"
)

// StreamLoader streams entity batches to Apache Doris using the Stream Load API.
// Each call to LoadPage opens one HTTP PUT transaction per page of results.
type StreamLoader struct {
	cfg    config.DorisConfig
	client *http.Client
}

// NewStreamLoader creates a StreamLoader configured for the given Doris target.
func NewStreamLoader(cfg config.DorisConfig) *StreamLoader {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify}, //nolint:gosec
	}
	return &StreamLoader{
		cfg:    cfg,
		client: &http.Client{Transport: transport},
	}
}

// LoadPage streams the given pre-flattened entities to Doris as a single
// Stream Load transaction. Each entity is serialised as one JSON line (NDJSON).
//
// The method is a no-op when entities is empty.
func (s *StreamLoader) LoadPage(ctx context.Context, entities []map[string]any) error {
	if len(entities) == 0 {
		return nil
	}

	// Normalise all entity keys to lower-case before encoding.
	// Doris column names are case-insensitive; if the source data contains
	// mixed-case variants of the same logical field (e.g. "fieldvalues.Cause"
	// and "fieldvalues.cause"), both would map to the same column and Doris
	// would reject the load with "Duplicate column". This mirrors the
	// normalisation already applied in schema.Infer for DDL generation.
	entities = normalizeEntityKeys(entities)

	body, err := encodeNDJSON(entities)
	if err != nil {
		return fmt.Errorf("doris: encode NDJSON: %w", err)
	}

	targetURL := fmt.Sprintf("%s/api/%s/%s/_stream_load",
		strings.TrimRight(s.cfg.Host, "/"), s.cfg.Database, s.cfg.Table)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("doris: create stream load request: %w", err)
	}

	// GetBody lets the HTTP client replay the body after a 307 redirect from
	// the Doris FE to the BE node.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))

	req.SetBasicAuth(s.cfg.User, s.cfg.Password)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("format", "json")
	req.Header.Set("read_json_by_line", "true")
	req.Header.Set("Expect", "100-continue")
	// jsonpaths / columns are intentionally omitted.
	// Without jsonpaths, Doris matches each JSON key to a table column by
	// direct string comparison (case-insensitive). A dot in a key name like
	// "project.id" is treated as a plain character, not a JSON path separator.
	// JSON keys that have no matching column in the table are silently ignored,
	// which means schema evolution (new fieldvalues.* keys appearing in the
	// source data after the table was created) never causes load failures.
	// Specifying an explicit columns list would break as soon as the source
	// data contains a key not yet present in the table.

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("doris: stream load request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("doris: read stream load response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Doris sometimes returns a 400 with an empty body but includes the
		// error detail in response headers (e.g. X-Doris-Error-Msg). Include
		// all headers in the error message to aid debugging.
		var hdr strings.Builder
		for k, vals := range resp.Header {
			hdr.WriteString(k)
			hdr.WriteString(": ")
			hdr.WriteString(strings.Join(vals, ", "))
			hdr.WriteString(" | ")
		}
		return fmt.Errorf("doris: stream load returned HTTP %d body=%q headers=[%s]",
			resp.StatusCode, string(respBody), strings.TrimSuffix(hdr.String(), " | "))
	}

	return parseStreamLoadResponse(respBody)
}

// parseStreamLoadResponse interprets the Doris Stream Load JSON result object.
// Doris returns {"Status": "Success", "NumberLoadedRows": N, ...} on success.
// "Publish Timeout" is also treated as success (data is committed but publish
// confirmation timed out — rows are still visible after a short delay).
func parseStreamLoadResponse(body []byte) error {
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		// Non-JSON body after a 2xx status — treat as success.
		return nil
	}

	status, _ := result["Status"].(string)
	switch status {
	case "Success", "Publish Timeout", "":
		return nil
	default:
		msg, _ := result["Message"].(string)
		return fmt.Errorf("doris: stream load failed with status %q: %s", status, msg)
	}
}

// encodeNDJSON serialises a slice of entity maps to newline-delimited JSON.
// Each map becomes one JSON object followed by a newline character.
func encodeNDJSON(entities []map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, entity := range entities {
		if err := enc.Encode(entity); err != nil {
			return nil, fmt.Errorf("entity %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// normalizeEntityKeys returns a new slice of entities with every key
// lower-cased. Doris column names are case-insensitive, so "fieldvalues.Cause"
// and "fieldvalues.cause" refer to the same column; sending both in one payload
// triggers a "Duplicate column" error. When two source keys collide after
// normalisation, the last value written into the map wins — both values should
// be identical for the same logical field, so the choice is inconsequential.
func normalizeEntityKeys(entities []map[string]any) []map[string]any {
	out := make([]map[string]any, len(entities))
	for i, entity := range entities {
		norm := make(map[string]any, len(entity))
		for k, v := range entity {
			norm[strings.ToLower(k)] = v
		}
		out[i] = norm
	}
	return out
}

// buildJSONMappings collects every unique JSON key present across all entities
// and returns two Doris Stream Load header values:
//
//   - jsonpaths: a JSON array of bracket-notation paths, e.g.
//     ["$['id']","$['project.id']","$['project.name']"]
//     Bracket notation treats the entire string as a literal key name, so
//     dots are not interpreted as JSON path separators.
//
//   - columns: a comma-separated list of backtick-quoted column names in the
//     same order as jsonpaths, e.g. `id`,`project.id`,`project.name`
//
// Both headers must be set together: jsonpaths defines extraction order and
// columns maps the results positionally to Doris table columns.
func buildJSONMappings(entities []map[string]any) (jsonpathsHeader, columnsHeader string) {
	keySet := make(map[string]struct{})
	for _, e := range entities {
		for k := range e {
			keySet[k] = struct{}{}
		}
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	paths := make([]string, len(keys))
	cols := make([]string, len(keys))
	for i, k := range keys {
		// Escape any single quotes in the key (defensive; rare in practice).
		escaped := strings.ReplaceAll(k, `'`, `\'`)
		paths[i] = "$['" + escaped + "']"
		cols[i] = "`" + k + "`"
	}

	pathsJSON, _ := json.Marshal(paths)
	return string(pathsJSON), strings.Join(cols, ",")
}
