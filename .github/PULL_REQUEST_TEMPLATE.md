<!--
Thanks for your contribution! Please fill in the sections below.
Delete any section that is not applicable.
-->

## Summary

<!-- One or two sentences: what does this PR do and why? -->

## Related Issue

<!-- Link the issue this PR closes, e.g. "Closes #123". Use "Refs #123" if it only partially addresses one. -->

Closes #

## Type of change

<!-- Check all that apply. Keep in sync with the Conventional Commit prefix. -->

- [ ] `feat` — new user-facing capability
- [ ] `fix` — bug fix
- [ ] `docs` — documentation only
- [ ] `refactor` — code change that neither fixes a bug nor adds a feature
- [ ] `perf` — performance improvement
- [ ] `test` — adding or improving tests
- [ ] `build` / `ci` — build system or CI change
- [ ] `chore` — tooling, deps, or maintenance
- [ ] **Breaking change** (existing `config.yaml` files or CLI flags stop working)

## Changes

<!--
Bullet the notable changes. For breaking changes, include a migration note
(what to change in config.yaml or CLI flags).
-->

-

## How was this tested?

<!--
Describe the testing you did:
- Which unit / integration / fuzz tests were added or updated?
- Any manual verification (e.g. against a real Doris FE, a specific source API)?
-->

-

## Contributor checklist

- [ ] Every commit is signed off (`git commit -s`) — DCO required.
- [ ] Every commit message follows [Conventional Commits](../blob/main/CONTRIBUTING.md#conventional-commits).
- [ ] Code is formatted (`gofmt -w .`) and passes `go vet ./...`.
- [ ] Tests added / updated; `go test -race ./...` passes locally.
- [ ] Documentation updated (README, godoc comments, config reference) where relevant.
- [ ] No secrets, credentials, or customer data in the diff.
- [ ] CI gates are green: **Test**, **Verify** (license, SBOM, Repolinter, TruffleHog).
- [ ] For breaking changes: migration notes included above **and** commit uses `!` or `BREAKING CHANGE:` footer.
