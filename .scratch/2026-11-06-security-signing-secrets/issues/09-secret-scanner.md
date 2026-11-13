# Secret scanner package

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: —
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-secret-scanner` | `.worktrees/feat+phase-17.3-secret-scanner` | `feat(secrets): secret scanner patterns (17.3)` | M |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

Nothing in Cloudzilla can recognise a secret. Every later ticket (push protection, backfill, the web editor) needs one pure function that says what a blob holds, so they cannot drift.

## Scope

- New package `internal/secretscan` (no database, no HTTP): `Scan(path string, content []byte) []Finding` with `Finding{Type, Line, Value, Confidence}`, a static `[]Pattern`, `Mask(value) string` (first and last four characters; for a private key the header line), `Hash(repoID int64, value string) string` (`sha256(repo_id ":" value)`, hex).
- The pattern table in the spec's Scanner section: `aws_access_key_id`, `private_key`, `cloudzilla_token`, `jwt_secret` (high) and `generic_api_key` (low). `Confidence` is `High` or `Low`; the scanner never decides what blocks.
- Skips: a NUL byte in the first 8 KiB (binary); content over `maxBytes` (a parameter; the caller passes `secret_scanning.max_blob_bytes`); at most 50 findings per blob.
- `cloudzilla_token` findings carry only the match; whether the token is live is the caller's lookup (ticket 14), so the package needs no store.
- `Value` is never logged: give `Finding` a `String()`/`LogValue()` that prints the type and line only.

## Files (hotspots in bold)

- **`internal/secretscan/patterns.go`** (new), `internal/secretscan/scan.go` (new), `internal/secretscan/scan_test.go` (new), `internal/secretscan/testdata/` (new)

## Acceptance criteria

- [x] Each pattern has a hit, a near-miss and a false-positive fixture in a table-driven test; `AKIAIOSFODNN7EXAMPLE` is found, `AKIA` + 15 characters is not
- [x] `Mask` never returns more than eight characters of the value; `Hash` differs across repo IDs and is stable across calls
- [x] A binary blob, an over-size blob and a 200-match blob behave as specified
- [x] No test or log line prints a `Value` (a test captures `slog` output for a scan of a file with a key)
- [ ] ~~Checked at desktop 1440 in light and dark~~ (no UI in this ticket)
- [ ] ~~The standing mocks in `mockups/` are updated~~ (no UI in this ticket)
- [ ] ~~Verified on a throwaway server~~ (pure package; unit tests are the verification)
- [x] `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Unit tests only: `go test ./internal/secretscan/...`. No server needed.

## Comments

