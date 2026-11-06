# Edit conflict recovery, branch commits and release assets

Created: 2026-11-06
Category: enhancement
Status: needs-triage
Blocked by: 11

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/edit-and-release-workflows` | `.worktrees/feat+edit-and-release-workflows` | `feat(repo): edit conflict recovery, branch commits and release assets` | polish (wave 1 re-score) | L | Features +0.4, Edge +0.2 |

## Problem

Found by the wave 1 re-score (Codex).

1. When a file edit hits a conflict, users are told to copy their changes, reopen the file and edit again; nothing
   shows the difference (`internal/view/pages/edit_file.templ:28`).
2. The edit form commits a plain textarea straight to the displayed ref: no diff preview, no new-branch or PR option
   (`edit_file.templ:60`, `:82`).
3. Release creation has no asset upload although the copy describes releases as packaging downloads
   (`internal/view/pages/release_new.templ:32`, `:102`).

## Scope

- Conflict: keep the user's text in the form and show the upstream change (diff) next to it. Share the
  keep-the-input mechanism with ticket 11.
- Edit: preview diff, and a "commit to a new branch and open a pull request" choice.
- Releases: asset upload and download list (storage backend already exists, see docs/storage.md).
- Needs a `/grill-with-docs` and `/to-spec` pass first.

## Files

- `internal/view/pages/edit_file.templ`, `release_new.templ`, `release_detail.templ`
- handlers and services for file edits and releases; `internal/db/migrations/` for release assets

## Acceptance criteria

- [ ] A conflicting edit shows the upstream diff and keeps the user's text
- [ ] A file edit can go to a new branch with a pull request
- [ ] A release can carry uploaded assets, listed on the detail page and served for download

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.
