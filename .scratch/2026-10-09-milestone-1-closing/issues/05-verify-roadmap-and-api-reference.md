# Verify the ROADMAP and API reference after #203

Created: 2026-10-09
Category: enhancement
Status: needs-triage

## Problem

#203 refreshed the README, ROADMAP and API reference. Before it, these were stale:

- ROADMAP lists Phase 8.3 (LDAP/SAML) as done and 19.1 (LDAP/SAML improvements) as planned.
- Milestone 2's table assigned migrations 053–060; the repo was already at 110.
- Mirrors, Prometheus metrics, admin password reset and `reset-2fa` were not tracked.
- The API reference listed about 186 endpoints in tables against roughly 350 routes registered in `internal/router/router.go` (the router count includes pages).
- `docs/` has no page for SSO, search, discussions or the wiki.

Not checked whether #203 covered all of this.

## Acceptance criteria

- [ ] Read the ROADMAP status table and the Milestone 1 closing note against `git log` and the migrations; fix what is still wrong.
- [ ] Mechanically diff the `/api/*` routes against the API reference: write a throwaway test in `internal/router` that walks the chi router with `chi.Walk` and prints method and path, then compare with the documented paths (treat `:param` and `{param}` as equal). List undocumented routes in the PR; document them or note why they are internal.
- [ ] Add short subsystem docs for SSO and the wiki if missing, and link them from CLAUDE.md's docs list.
- [ ] Mark Milestone 1 closed in the ROADMAP once tickets 01–03 and 06 are done.

## Blocked by

Nothing.
