# Merged pull requests keep their commits and diff

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/merged-pr-keeps-diff` | `.worktrees/fix+merged-pr-keeps-diff` | `fix(ui): merged pull requests keep their commits and diff` | polish (wave 2 re-score) | M | Edge +0.3, Features +0.2 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. A merged PR reads "merged 0 commits into main" and its Files tab says "No changes." (Claude, `pull-merged`, `pull-merged-files`; Codex saw the same, marked unverified). The commit count and diff are computed live from head and base, and once the head is merged into base both are empty, so the history a merged PR exists to show is gone.
2. The Commits and Files tabs show no counts on a merged PR.

## Scope

- Persist the head and base SHAs (or the merge base) when a PR merges, and build its commit list, count and diff from them.
- Backfill existing merged PRs where the objects are still reachable; otherwise say "Diff unavailable after merge" instead of "No changes."
- Check ticket 30 (merger and count in the header): its wording is right, the number it is given is not.

## Files

- `internal/service/pull_service.go`, merge path
- `internal/view/fragments/pull_chrome.templ`, `pages/pr_files.templ`
- `mockups/pr.html`, `pr_files.html`

## Acceptance criteria

- [x] A merged PR shows its original commit count and diff after the head branch is merged or deleted
- [x] Old merged PRs say why when the diff cannot be rebuilt
- [x] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Record what a merged PR showed (head and base SHAs at merge) rather than recomputing from live refs; read docs/pr-merge.md.
