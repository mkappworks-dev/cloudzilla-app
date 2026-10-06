# Superadmin role is read fresh on each session request

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: 01

## What to build

`sessionLive` already reads the user row per request. Make that lookup (`SessionVersions` interface, `UserStore.SessionVersion`) also return `is_superadmin`, and overwrite `claims.IsSuperadmin` with it on both `Auth` and `OptionalAuth`. `RequireSuperadmin` and every handler's `claims.IsSuperadmin` check then see the fresh value. PAT and OAuth claims keep `IsSuperadmin` false.

## Acceptance criteria

- [x] Promoting or demoting a user takes effect on their next request, without signing them out.
- [x] Token claims never carry `IsSuperadmin`.

## Tests

- Middleware: a JWT claiming `is_superadmin: true` for a user the lookup reports as not superadmin yields claims with `IsSuperadmin == false`, and the reverse.
