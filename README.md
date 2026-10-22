# doris-rest-loader

A containerised Go tool that ingests data from arbitrary RESTful endpoints into [Apache Doris](https://doris.apache.org/) via the [Stream Load API](https://doris.apache.org/docs/data-operate/import/stream-load-manual).

Inspired by the simplicity of [doris-streamloader](https://github.com/apache/doris-streamloader), but designed for RESTful data sources rather than files.

---

## Features

- **Flexible authentication** — No-auth, Basic, Bearer, Preflight Bearer (username/password → token), OAuth2 (Client Credentials & Password grant)
- **Configurable pagination** — Page-number, offset, and cursor (sequential keyset, e.g. Bitbucket's nextPageStart/isLastPage) strategies, with path/query-parameter injection
- **Concurrent page fetching** — Configurable goroutine pool with exponential-backoff retry
- **Automatic data flattening** — Nested JSON objects are flattened to dot-notation keys by default; optional field selection and column renaming
- **Doris Stream Load** — NDJSON format, one HTTP transaction per page, redirect-aware (FE→BE)
- **Container-native** — Single binary in a distroless image; configuration injected via a YAML file, ideal for Kubernetes CronJobs

---

## Quick Start

### Docker

```bash
docker run --rm \
  -v $(pwd)/my-config.yaml:/etc/doris-rest-loader/config.yaml \
  ghcr.io/e-solutions-gmbh/doris-rest-loader
```

### Binary

```bash
doris-rest-loader --config ./my-config.yaml
```

---

## Configuration Reference

```yaml
# ── Source ────────────────────────────────────────────────────────────────────
source:
  # Required. Use {placeholder} syntax for path-injected pagination parameters.
  url: "https://api.example.com/v1/items/{page}/{limit}"
  # HTTP method (default: GET)
  method: GET
  # Static headers merged into every request.
  headers:
    Accept: application/json
    X-Api-Version: "2"
  # Static query parameters merged into every request URL.
  query_params:
    format: json
  # Dot-notation path to the entity array within the response body.
  # Defaults to the response root (handles both array and object roots).
  data_path: "data.items"
  # Disable TLS certificate verification (development only).
  tls_skip_verify: false
  # Optional. Switches the runner into a two-stage "list, then fan out a
  # parameterised call per list entry" fetch mode. Absent (the default) is
  # fully backward compatible with the single-stage paginated fetch above.
  # See "Fan-Out Source Mode" below for the full semantics.
  fanout:
    list_url: "https://api.example.com/v1/items?type=X"
    list_data_path: "items"
    # Polymorphic field spec: shorthand "key" or an ordered mapping of
    # placeholder -> dot-notation path. Each entry is substituted into
    # "{placeholder}" in source.url AND auto-injected as an output column
    # (named after the placeholder) into every entity from that item's
    # detail response(s) — useful when the detail endpoint doesn't echo
    # the identifier back in its body.
    item_fields:
      item: "key"

# ── Authentication ─────────────────────────────────────────────────────────────
auth:
  # Supported types: noauth (default), basic, bearer, preflight_bearer, oauth2
  type: preflight_bearer
  url: "https://auth.example.com/api/login"
  username: "my-user"
  password: "my-password"

# Other auth examples:
#
# Basic Auth:
#   auth:
#     type: basic
#     username: "user"
#     password: "pass"
#
# Static Bearer token:
#   auth:
#     type: bearer
#     token: "eyJhbGci..."
#
# OAuth2 – Client Credentials:
#   auth:
#     type: oauth2
#     grant_type: client_credentials
#     client_id: "my-client"
#     client_secret: "my-secret"
#     token_url: "https://auth.example.com/oauth/token"
#     scopes: ["read:data"]
#
# OAuth2 – Password Grant (deprecated in OAuth 2.1):
#   auth:
#     type: oauth2
#     grant_type: password
#     client_id: "my-client"
#     client_secret: "my-secret"
#     token_url: "https://auth.example.com/oauth/token"
#     username: "user"
#     password: "pass"

# ── Pagination ────────────────────────────────────────────────────────────────
pagination:
  # Supported types: page_number (default), offset, cursor
  type: page_number
  # How pagination parameters are injected: path (default), query_params.
  # body is not yet implemented for any pagination type.
  injection: path
  # First page number (default: 1)
  start_page: 1
  # URL placeholder names (default: page / limit)
  page_param: page
  limit_param: limit
  # Number of entities per page (default: 100)
  page_size: 50
  # Maximum number of pages to fetch per run (default: 0 = unlimited)
  max_pages: 10
  # Dot-notation paths to pagination metadata in the response.
  # page_number type requires EITHER num_pages_path
  # OR both total_entries_path + page_size.
  num_pages_path: "meta.totalPages"
  total_entries_path: "meta.totalCount"   # alternative / additional
  current_page_path: "meta.currentPage"   # informational only

# Offset example:
#   pagination:
#     type: offset
#     injection: path
#     offset_param: offset
#     limit_param: limit
#     page_size: 100
#     total_entries_path: "meta.total"

# Cursor example (e.g. Bitbucket Server's nextPageStart/isLastPage shape).
# Cursor pagination is inherently sequential — one page at a time, never
# fetched through the concurrent page pool — because each page's URL
# depends on the previous page's response:
#   pagination:
#     type: cursor
#     injection: query_params
#     cursor_param: "start"      # query param / path placeholder for the cursor
#     limit_param: "limit"
#     page_size: 1000
#     cursor_path: "nextPageStart"   # cursor value read from each response
#     last_page_path: "isLastPage"   # optional explicit "done" signal;
#                                     # when omitted, an absent/empty
#                                     # cursor_path value terminates pagination

# ── Threading & Retry ─────────────────────────────────────────────────────────
threading:
  # Maximum concurrent page-fetching goroutines (default: 5)
  max_goroutines: 10
  retry:
    max_attempts: 3          # default: 3
    initial_delay: 1s        # default: 1s
    multiplier: 2.0          # default: 2.0
    max_delay: 30s           # default: 30s

# ── Data Flattening ───────────────────────────────────────────────────────────
flattening:
  # Recursively flatten nested objects into dot-notation keys (default: true)
  enabled: true
  # Key separator (default: ".")
  separator: "."
  # Optional field selection. When set, only listed fields are emitted.
  # Each selected value is still flattened if enabled: true.
  include:
    - path: "user.id"
      as: "user_id"          # optional column rename
    - path: "user.email"     # no rename → column name = "user.email"
    - path: "score"
  # Optional. Adds a column with this name containing the full original
  # entity as JSON, for reprocessing later without re-fetching (default: unset).
  raw_json_column: "raw_json"

# ── Apache Doris Target ───────────────────────────────────────────────────────
doris:
  host: "http://doris-fe:8030"
  database: "analytics"
  table: "events"            # configurable per endpoint
  user: "root"               # default: root
  password: ""
  tls_skip_verify: false
```

---

## Fan-Out Source Mode

Many REST APIs expose data as a two-level hierarchy: a "list" endpoint
enumerates a set of parent entities/identifiers, and the data of interest only
lives behind a second "detail" endpoint, parameterised by each identifier
(e.g. `GET /items?type=X` → `[{id: "a"}, {id: "b"}, ...]`, then
`GET /items/{id}/details` once per `id`). `source.fanout` adds first-class
support for this shape without requiring N hand-written configs.

When `source.fanout` is present, the runner performs a two-stage fetch instead
of the single-stage paginated fetch:

- **Stage 1 — List fetch.** `fanout.list_url` is fetched, optionally paginated
  via `fanout.list_pagination` (same schema as the top-level `pagination`
  block — omit it if the list endpoint returns every identifier in one
  response). `fanout.item_fields` selects one or more dot-notation fields
  (within one list entity) and deduplicates the resulting tuples into a set
  of fan-out items.
- **Stage 2 — Detail fetch per item.** For each fan-out item, every selected
  value is substituted into its own `{placeholder}` occurrence in
  `source.url`. Each resulting per-item request runs through the existing
  single-endpoint pipeline unchanged — including the top-level `pagination`
  block, which now paginates **each item's detail call independently**
  rather than the list call. Per-item fetches run concurrently, bounded by
  `threading.max_goroutines`; a single failing item is logged and skipped
  rather than aborting the whole run (same fail-and-continue contract as
  per-page failures).

`fanout.item_fields` is a polymorphic field spec supporting two YAML shapes:

```yaml
# shorthand — a single field; the placeholder/column name is the path's
# leaf segment (e.g. "project.key" -> placeholder "key").
item_fields: "slug"

# mapping — one or more fields, each with an explicit placeholder/column
# name. Declaration order is preserved.
item_fields:
  projectKey: "project.key"
  slug: "slug"
```

Every `item_fields` entry (and, when `sub_list` is configured,
`first_item_fields`/`last_item_fields` — see below) serves **two purposes at
once**, with no separate rename step:

1. It is substituted into `"{placeholder}"` in `source.url` (and, when
   `sub_list` is configured, into `sub_list.list_url`).
2. It is **auto-injected as an output column** named after the placeholder
   into every entity extracted from that item's detail response(s) — on
   every page, not just the first — before flattening. This is essential
   when the detail endpoint doesn't echo the identifier back in its
   response body (e.g. SonarQube's
   `/api/measures/search_history?component={key}&...`, which returns measure
   history but no `component`/project-key field): without it, rows from
   different items would be indistinguishable once flattened and streamed
   to Doris.

```yaml
source:
  # {item} is substituted per fan-out item; may be combined with the
  # top-level pagination block's own placeholders.
  url: "https://api.example.com/v1/items/{item}/details"
  method: GET
  data_path: "data.items"      # applies to each per-item detail response
  tls_skip_verify: true

  fanout:
    # Required. Endpoint that enumerates the parent entities.
    list_url: "https://api.example.com/v1/items?type=X"
    # Dot-notation path to the array of parent entities in the list response.
    # Defaults to the response root, same convention as source.data_path.
    list_data_path: "items"
    # Required. One or more dot-notation fields to fan out on, each both a
    # URL placeholder and an auto-injected output column (see above).
    item_fields:
      item: "key"
    # Optional: pagination for the list fetch itself. Omit if the list
    # endpoint returns everything in a single response. Same schema/semantics
    # as the top-level `pagination` block.
    list_pagination:
      type: page_number
      injection: query_params
      page_param: "p"
      limit_param: "ps"
      page_size: 500
      total_entries_path: "paging.total"

# Applies to EACH per-item detail call (stage 2), independently per item —
# not to the list call.
pagination:
  type: page_number
  injection: query_params
  page_param: "p"
  limit_param: "ps"
  page_size: 100
  total_entries_path: "paging.total"
```

### Nested paginated sub-list (`fanout.sub_list`)

Some fan-out use cases need more than a flat list of identifiers per item.
For example, computing a Bitbucket repository's net lines-of-code churn
since creation requires, per repository: fetching its full (paginated) commit
history, then combining the **first commit of the first page** (HEAD) and the
**last commit of the last page** (the repo's root commit) into the final
detail request.

`source.fanout.sub_list`, when set, runs this extra fetch per fan-out item
**before** the final detail request:

1. `sub_list.list_url` (with the outer item's `item_fields` placeholders
   already substituted in) is fetched, walking every page via
   `sub_list.pagination` — reusing the same `pagination.Adapter` machinery
   as the top-level `pagination` block, including the `cursor` type (see
   "Pagination" above), which is the natural fit for APIs like Bitbucket
   Server's `isLastPage`/`nextPageStart` commit history.
2. `sub_list.first_item_fields` is extracted from the **first entity of the
   first page**; `sub_list.last_item_fields` from the **last entity of the
   last page** (both are `FieldSpec`s, with the same shorthand/mapping
   shapes as `fanout.item_fields`).
3. Both sets of values are merged into the item's values — available as
   `"{placeholder}"` substitutions in `source.url` and auto-injected as
   output columns, exactly like `fanout.item_fields`.

An empty sub-list result for a given item (e.g. zero commits) is treated as
a hard error for that item, propagated through the same per-item
failure/aggregation mechanism as any other item-level failure — there is no
silent empty-placeholder fallback. The sub-list fetch reuses the run's
existing fetcher/auth/retry configuration; it is not a separate auth
context. Walking a single item's sub-list pages is always sequential
(never parallelized), exactly like cursor pagination in general —
concurrency is only ever applied across fan-out items
(`threading.max_goroutines`).

```yaml
source:
  url: "https://bitbucket.example.com/rest/api/latest/projects/{projectKey}/repos/{slug}/compare/diff-stats-summary?from={head}&to={root}"
  fanout:
    list_url: "https://bitbucket.example.com/rest/api/latest/repos?limit=1000"
    list_data_path: "values"
    item_fields:
      projectKey: "project.key"
      slug: "slug"
    sub_list:
      list_url: "https://bitbucket.example.com/rest/api/latest/projects/{projectKey}/repos/{slug}/commits?limit=1000"
      list_data_path: "values"
      pagination:
        type: cursor
        injection: query_params
        cursor_param: "start"
        limit_param: "limit"
        page_size: 1000
        cursor_path: "nextPageStart"
        last_page_path: "isLastPage"
      first_item_fields:
        head: "id"
      last_item_fields:
        root: "id"
```

`--dry-run` / `DRY_RUN=1` also supports `source.fanout` (including
`sub_list`): it fetches the list, takes only the first fan-out item, runs
stage 2 for that single item, and prints the inferred DDL from its first
detail page — the fan-out analogue of today's "first page only" dry-run
semantics.

See [`examples/fanout-example.yaml`](examples/fanout-example.yaml) for a
complete, generic example, and
[`examples/fanout-sublist-bitbucket-loc.yaml`](examples/fanout-sublist-bitbucket-loc.yaml)
for the full Bitbucket `sub_list` + cursor pagination example above.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  doris-rest-loader                                              │
│                                                                 │
│  Config ──► Runner                                              │
│              │                                                  │
│              ├─ Auth Adapter    (Prepare preflight token)       │
│              │                                                  │
│              ├─ Fetcher ──► Page 1 (sync)                       │
│              │     │           │                                │
│              │     │         Extract entities (data_path)       │
│              │     │         Flatten entities                   │
│              │     │         Stream ──► Doris Stream Load       │
│              │     │                                            │
│              │     └─► Evaluate pagination (total pages)        │
│              │                                                  │
│              └─ Goroutine pool (pages 2..N)                     │
│                    │  ┌──────────────────────────────┐          │
│                    ├──► goroutine: fetch + flatten ──► channel  │
│                    ├──► goroutine: fetch + flatten ──► channel  │
│                    └──► goroutine: fetch + flatten ──► channel  │
│                                                       │         │
│                         Reader ◄──────────────────────┘         │
│                            │                                    │
│                            └──► Doris Stream Load (per page)    │
└─────────────────────────────────────────────────────────────────┘
```

---

## Running on Kubernetes

Example CronJob that runs the ingestion every hour:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: doris-rest-loader-events
  namespace: data-ingestion
spec:
  schedule: "0 * * * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: loader
              image: ghcr.io/e-solutions-gmbh/doris-rest-loader:latest
              args: ["--config", "/etc/loader/config.yaml"]
              volumeMounts:
                - name: config
                  mountPath: /etc/loader
                  readOnly: true
          volumes:
            - name: config
              secret:
                secretName: doris-rest-loader-events-config
```

Store your `config.yaml` as a Kubernetes Secret (it may contain credentials):

```bash
kubectl create secret generic doris-rest-loader-events-config \
  --from-file=config.yaml=./config.yaml \
  -n data-ingestion
```

---

## Development

### Prerequisites

- Go 1.24+
- Docker (for image builds)

### First-time setup

```bash
# Clone and enter the repository
git clone https://github.com/e-solutions-GmbH/doris-rest-loader.git
cd doris-rest-loader

# Fetch dependencies and generate go.sum
go mod tidy

# Verify everything builds
go build ./...
```

### Running tests

```bash
# Run all tests with the race detector
go test -v -race ./...

# Run a specific package
go test -v ./internal/flattener/...
```

The suite is fully hermetic: the end-to-end integration tests in
`internal/runner/integration_test.go` spin up in-process `httptest` mock servers
for both the source API and the Doris Stream Load endpoint, so **no Docker or
live Doris instance is required**. A normal `go test ./...` run also executes
every fuzz target's seed corpus as regular test cases.

#### Fuzz tests

Several parsing-heavy seams have native Go fuzz targets
(`FuzzExtractEntities`, `FuzzRunnerPipeline`, `FuzzFlatten`, `FuzzBuildURL`).
Run one for a bounded time to search for new crashers:

```bash
# Fuzz the pagination URL builder for 30s
go test ./internal/pagination/ -run '^ -fuzz '^FuzzBuildURL -fuzztime 30s
```

#### Coverage report

```bash
# Generate a coverage profile and print the total
go test -race -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1

# Optional: open an annotated HTML report
go tool cover -html=coverage.out
```

### Building the Docker image locally

```bash
docker build -t doris-rest-loader:dev .
docker run --rm \
  -v $(pwd)/example-config.yaml:/etc/doris-rest-loader/config.yaml \
  doris-rest-loader:dev
```

### Release process

1. Merge your changes to `main` — a `latest` image is published automatically.
2. Tag a release to publish a versioned image:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

---

## Project Structure

```
doris-rest-loader/
├── cmd/doris-rest-loader/        # Binary entry point
├── internal/
│   ├── auth/                     # Auth adapters (noauth, basic, bearer, preflight, oauth2)
│   ├── config/                   # YAML config structs, loader, defaults, validation
│   ├── doris/                    # Doris Stream Load client
│   ├── fanout/                   # Fan-out item extraction and URL substitution
│   ├── fetcher/                  # HTTP fetcher with retry/backoff
│   ├── flattener/                # Entity flattening and field selection
│   ├── jsonpath/                 # Minimal dot-notation JSON path resolver (no deps)
│   ├── pagination/               # Pagination adapters (page_number, offset, cursor)
│   └── runner/                   # Ingestion pipeline orchestration
├── .github/
│   ├── workflows/
│   │   ├── test.yml              # CI: build, gofmt, race tests, fuzz smoke
│   │   ├── verify.yml            # PR gate: license, SBOM, Repolinter, secret scan
│   │   └── docker.yml            # CD: build & push image on main / version tag
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.yml        # Bug report form
│   │   ├── feature_request.yml   # Feature request form
│   │   └── config.yml            # Issue chooser (disables blank, routes security)
│   ├── PULL_REQUEST_TEMPLATE.md  # PR checklist (DCO, CC, tests, CI gates)
│   └── repolinter.json           # Custom Repolinter ruleset used by verify.yml
├── CODE_OF_CONDUCT.md            # Contributor Covenant v2.1
├── CONTRIBUTING.md               # DCO, Conventional Commits, workflow
├── SECURITY.md                   # Private vulnerability reporting
├── .pre-commit-config.yaml       # Conventional-Commits commit-msg hook
├── Dockerfile
├── go.mod
├── LICENSE
└── README.md
```

---

## Implementation Notes

| Topic | Decision |
|---|---|
| Doris ingest format | NDJSON with `read_json_by_line: true` — better than CSV for semi-structured data |
| Redirect handling | `GetBody` set on PUT requests so Go HTTP client replays body on 307 FE→BE redirect |
| YAML parsing | `gopkg.in/yaml.v3` — only external dependency alongside `golang.org/x/oauth2` |
| Logging | `log/slog` (stdlib, Go 1.21+) with JSON output for Kubernetes compatibility |
| Cursor pagination | Fully implemented keyset/sequential adapter (`internal/pagination/cursor.go`); inherently sequential since each page's URL depends on the previous page's response, so it bypasses the concurrent goroutine pool at every level (top-level `pagination`, fan-out list, `sub_list`, per-item detail) |

---

## License

[MIT](LICENSE) © 2026 e-solutions GmbH
