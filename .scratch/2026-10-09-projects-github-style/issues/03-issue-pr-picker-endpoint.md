# Issue/PR search endpoint for the card composer

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] JSON endpoint returns this repo's issues and PRs matching a title fragment or number (limited, newest first), with kind, number, title and state.
- [ ] Reuses an existing search/service if one fits; respects repo read access.
- [ ] Tests cover title match, number match and cross-repo isolation.
