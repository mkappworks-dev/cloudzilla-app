# Suggestion queries in the store and service

Created: 2026-10-08
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

- `SearchStore.SuggestRepos`, `SuggestUsers`, `SuggestOrgs` in `internal/store/search_store.go`, with a shared `likePrefix` helper that escapes `\`, `%` and `_` and is used with `LIKE ... ESCAPE '\'`.
- `SuggestRepos` matches `lower(name)` or `lower(owner_name || '/' || name)`, adds `deleted_at IS NULL` and `readableBy("r", "$2")`, and orders exact-name matches first, then `updated_at DESC`.
- `SearchService.Suggest(ctx, q, viewerID *int64) (*Suggestions, error)`: trim, empty result under 2 characters, truncate to 100, run the three queries in an `errgroup`, drop a failed group.

## Acceptance criteria

- [ ] Integration test (modelled on `search_visibility_test.go`): a private repo is suggested to its owner and to a collaborator, not to a stranger or an anonymous viewer; a soft-deleted repo to nobody.
- [ ] `q` of `%` or `_` matches nothing instead of everything.
- [ ] The ghost user is never suggested; orgs match on `name` and `display_name`.
- [ ] Per-type limits are respected.
- [ ] `Suggest` returns empty for a one-character query without touching the store.

## Blocked by

None.
