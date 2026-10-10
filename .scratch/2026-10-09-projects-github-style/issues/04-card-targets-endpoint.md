# Issue/PR search endpoint for the `#` picker

Created: 2026-10-09
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [x] `GET /card-targets?q=` returns the repo's issues and PRs matching a title fragment or `#number`, newest first, limit 8, with id, kind, number, title and state.
- [x] Requires write access; a typed `%` or `_` matches literally.
- [x] Tests cover title match, number match, literal wildcards, empty query, anonymous and outsider.

Note: already written and passing in the working tree (`ProjectStore.SearchCardTargets`, `ProjectService.SearchCardTargets`, `TestProjects_CardTargets`); keep it.
