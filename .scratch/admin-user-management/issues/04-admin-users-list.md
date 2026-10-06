# `/admin/users` list

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

Blocked by: 01

## What to build

A new admin tab, `GET /admin/users`, behind `authMW` and `superadminMW`. `UserStore.ListForAdmin(ctx, filter, page, perPage)` returns rows and a total: 50 per page, newest first, never the ghost. `q` matches a username or email prefix case-insensitively (`lower(username) LIKE lower($q)||'%'`, with `%`/`_` escaped). Filters: role (`all`, `superadmin`) and status (`all`, `active`, `suspended`). Columns: username (links to `/admin/users/{username}`), name, email, role, status, 2FA, sign-in method (password, Google, LDAP or SAML), created date. Pagination follows `/admin/audit-log`.

## Acceptance criteria

- [ ] `/admin/users` lists every account but the ghost, 50 per page, with total count, `q` prefix search on username and email, and role and status filters; non-superadmins get `403`.

## Tests

- Store: filters, prefix search (including `%` in `q`), ordering, total, ghost excluded.
- Handler: non-superadmin gets `403`; superadmin sees a seeded user.
