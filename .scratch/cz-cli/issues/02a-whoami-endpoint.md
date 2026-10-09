# GET /api/user: who does this token belong to

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: none

Part of [spec](../spec.md). Found while starting 02: `cz auth login` must verify a token and learn the username in one call, and no endpoint does that.

- `GET /api/users/{username}` needs the username as input.
- `GET /api/repos/` is behind `optAuthMW`, returns a bare array, and may answer a bad token with the public list instead of `401`.
- `/api/user/*` is closed to scoped tokens in `internal/middleware/scope.go`.

## What to build

`GET /api/user` returns `{"id": N, "username": "…"}` for the caller, `401` for no or invalid credentials (`authMW`, not `optAuthMW`).

- Open to PATs and OAuth-app tokens with any of `readScopes` (add the route to the allow-list in `internal/middleware/scope.go`); a JWT session works as for every route.
- JSON only; no HTMX variant. No email or other private fields: a token with only `repo:read` must not learn more than `GET /api/users/{username}` shows the public.
- Layering as in `CLAUDE.md`: route in `internal/router/router.go`, handler in `internal/handler/`, `writeJSON`.
- Does not collide with the closed `/api/user/*` prefix: only the exact path `/api/user` opens, and every `/api/user/…` route stays closed to tokens.

## Acceptance criteria

- [ ] `GET /api/user` with a valid PAT of each read scope returns `{id, username}`.
- [ ] A missing, malformed, expired or revoked token gets `401`, never an anonymous result.
- [ ] A suspended account's token is refused as on every other authenticated route.
- [ ] `/api/user/tokens`, `/api/user/keys` and the rest of `/api/user/*` are still `403 insufficient_scope` for a PAT; a test pins this.
- [ ] `docs/api-reference.md` lists the endpoint and `docs/access-control.md`'s scope table shows it open to the read scopes.
- [ ] Handler and `scope.go` tests cover the cases above.

## Comments
