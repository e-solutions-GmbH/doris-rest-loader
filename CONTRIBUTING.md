# Contributing to doris-rest-loader

Thanks for contributing! By participating you agree to our [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to Contribute

- Report bugs via [Bug Report](.github/ISSUE_TEMPLATE/bug_report.yml)
- Request features via [Feature Request](.github/ISSUE_TEMPLATE/feature_request.yml)
- Improve docs, tests, or submit PRs

All discussion happens on GitHub (Issues + Discussions). No external channels.

## Security

See [SECURITY.md](SECURITY.md). Do **not** file public issues for vulnerabilities.

## Development Setup

**Prerequisites:** Go 1.24+, Git, Docker (optional), [`pre-commit`](https://pre-commit.com/).

```bash
git clone https://github.com/e-solutions-GmbH/doris-rest-loader.git
cd doris-rest-loader
go mod tidy && go build ./... && go test -race ./...
```

The test suite is fully hermetic.

## Workflow

Fork → feature branch off `main` → PR to `main`.

1. Branch prefixes: `feat/`, `fix/`, `docs/`, `refactor/`, `test/`, `chore/`.
2. Sign off every commit (`git commit -s`) — see [DCO](#dco).
3. Follow [Conventional Commits](#conventional-commits).
4. Rebase on `main` (prefer over merge commits).
5. Fill in the PR template; mark WIP as **Draft**.
6. All CI gates must pass; **1 maintainer approval** required.
7. Maintainers squash-merge.

## Conventional Commits

Format: `<type>(<scope>): <summary>`

Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

Breaking changes: append `!` or add a `BREAKING CHANGE:` footer.

Example:

```
feat(pagination): add cursor-based pagination adapter

Signed-off-by: Your Name <you@example.com>
```

Enforced locally by [`.pre-commit-config.yaml`](.pre-commit-config.yaml) and in CI.

## DCO

We use the [Developer Certificate of Origin](https://developercertificate.org/). No CLA. Sign off every commit:

```bash
git commit -s -m "feat(fetcher): add per-request timeout"
```

Backfill with `git rebase --signoff main`. Unsigned commits block merge.

## Pre-commit Hooks

One-time setup:

```bash
pipx install pre-commit   # or: pip install --user pre-commit / brew install pre-commit
pre-commit install --hook-type commit-msg
```

Every `git commit` then validates the message against Conventional Commits.

## Code Style & Testing

- Format: `gofmt -w .` (CI enforces).
- Vet: `go vet ./...`.
- Test: `go test -race -count=1 ./...`.
- Coverage: `go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1`.
- Add regression tests for bug fixes; add fuzz targets for new parsers.

## CI Gates

Required checks on every PR:

- **Test** (`.github/workflows/test.yml`) — build, gofmt, race tests, fuzz smoke.
- **Verify** (`.github/workflows/verify.yml`) — license compliance (`go-licenses`), CycloneDX SBOM, Repolinter, TruffleHog secret scan.

Fix the underlying issue rather than bypassing a gate.

## License

Contributions are licensed under the [MIT License](LICENSE). By submitting a PR with a DCO sign-off, you certify you have the right to do so.
