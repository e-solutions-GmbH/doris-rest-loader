# Security Policy

## Supported Versions

Only the latest release and the `main` branch receive security fixes.

| Version           | Supported          |
| ----------------- | ------------------ |
| Latest release    | :white_check_mark: |
| `main` branch     | :white_check_mark: |
| Older releases    | :x:                |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Please report via one of the following private channels:

1. **GitHub Private Vulnerability Reporting** (preferred) — open a report at
   [Security → Report a vulnerability](https://github.com/e-solutions-GmbH/doris-rest-loader/security/advisories/new).
2. **Email** — [info@esolutions.de](mailto:info@esolutions.de) with the subject
   line `[SECURITY] doris-rest-loader: <short description>`.

Please include as much of the following as you can:

- Affected version, image tag, or commit SHA
- A clear description of the issue and its impact
- Steps to reproduce (a minimal, sanitized configuration if applicable)
- Any proof-of-concept, logs, or screenshots (with secrets redacted)
- Your name / handle for credit (optional)

## Our Commitment

- **Acknowledgement:** within **3 business days** of receipt.
- **Triage & initial assessment:** within **10 business days**.
- **Fix timeline:** proportional to severity (CVSS-based). Critical issues are
  prioritized for the next patch release.
- **Coordinated disclosure:** we aim to publish a fix and a GitHub Security
  Advisory within **90 days** of the initial report, or sooner where practical.
  We will coordinate the disclosure timeline with you.

## Scope

In scope:

- Source code in this repository (`cmd/`, `internal/`)
- The published container image (`ghcr.io/e-solutions-gmbh/doris-rest-loader`)
- CI/CD workflows in `.github/workflows/`

Out of scope:

- Third-party dependencies (please report to the upstream project; we will update the dependency once a fix is available)
- Vulnerabilities that require an already-compromised environment (e.g. a malicious `config.yaml` provided by an already-privileged user)
- Denial of service via unbounded input from a fully trusted source API — the loader is designed to be operated against trusted, configured endpoints

## Safe Harbor

We consider security research conducted in good faith, in accordance with this
policy, to be authorized. We will not pursue legal action against researchers
who:

- Make a good-faith effort to avoid privacy violations, data destruction, and
  service disruption,
- Only interact with accounts / systems they own or have explicit permission
  to test,
- Report the vulnerability privately as described above, and
- Give us reasonable time to remediate before any public disclosure.

Thank you for helping keep doris-rest-loader and its users safe.
