# `#123` autolinks in card descriptions

Created: 2026-10-09
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [x] A repo-scoped goldmark transformer in `internal/markdown` links `#N` to `/issues/N`, or `/pulls/N` when only a PR has that number; unknown numbers stay plain text.
- [x] Not applied inside code spans or fenced code, and not inside existing links.
- [x] The board renders descriptions through it; tests cover issue, PR, unknown number, code span and an existing link.
