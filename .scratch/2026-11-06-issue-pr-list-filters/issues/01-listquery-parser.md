# Qualifier parser for issue and PR lists

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: none

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/issue-pr-list-filters` | `.worktrees/feat+issue-pr-list-filters` | `feat(issues,pulls): qualifier search, filter menus and pagination` (all three tickets) |

Spec: [../spec.md](../spec.md). This PR also carries the spec, the tickets and the mock folder.

## Scope

- New package `internal/listquery`: `Parse(raw string) Query`, `Query.String()` in canonical order, `With` / `Without`
  helpers for chip and menu links, and alias folding for the legacy `state=`, `label=`, `milestone=` and `sort=` params.
- Qualifiers: `is:`, `author:`, `assignee:`, `label:` (repeatable), `milestone:`, `no:`, `sort:`, free text. Quoted
  values (`label:"good first issue"`). Unknown `key:value` stays free text.
- `GLOSSARY.md`: add "Qualifier".

## Acceptance criteria

- [x] Table-driven tests cover each qualifier, quoting, `@me`, repeated labels, unknown keys, round-trip
      `Parse(q.String()) == q`, and the legacy aliases
- [x] `Without` removes exactly one qualifier value and keeps the rest in canonical order
- [x] `GLOSSARY.md` has "Qualifier"
- [x] No handler, store or view changes

## Verification

`go test ./internal/listquery/...`; `/code-review` before the PR.
