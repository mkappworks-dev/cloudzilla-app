# cz skeleton, token store and auth commands

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 01

Part of [spec](../spec.md).

## What to build

`cmd/cz` with a cobra root, a small API client package, a token store, and these commands:

- `cz auth login [--host URL] [--with-token]`: prompt for a PAT (hidden input) or read stdin; call one authenticated endpoint to verify it and learn the username; save host + token.
- `cz auth status`: host, user, where the token is stored; non-zero exit when logged out.
- `cz auth logout`.
- `cz api <METHOD> <path> [--field k=v] [--input file]`: authenticated raw request, prints the response body.

API client: resolves host and token from `CZ_HOST` / `CZ_TOKEN`, then the store; sets `Authorization: Bearer`; maps `401`, `403 insufficient_scope` (print the scope from `WWW-Authenticate`), `429` (honour `Retry-After`), and network failures to one-line errors. Forms or JSON per endpoint as the API requires; check that the endpoints used return JSON, not an HTMX fragment, for a request without `HX-Request`.

Token store: OS keychain; fallback to a `0600` file under the user config dir; `--insecure-storage` forces the file. Never print the token.

`cz` must not import `internal/store` or `internal/service`. Decide the client package's location here and record it in the spec.

## Acceptance criteria

- [ ] Login with a good PAT saves it and `status` reports the user; a bad token saves nothing and exits non-zero.
- [ ] `CZ_TOKEN` + `CZ_HOST` work with no stored login.
- [ ] Keychain unavailable → file fallback with mode `0600`; `status` says which.
- [ ] `403 insufficient_scope` names the missing scope; `401` suggests `cz auth login`.
- [ ] `cz api GET /api/repos/` works against a test server.
- [ ] Tests run the client against `httptest` servers; a test fails if `cmd/cz` imports `internal/store` or `internal/service`.

## Comments
