# Apply suggestion and applied state

Created: 2026-11-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-suggestion-apply` | `.worktrees/feat+pr-suggestion-apply` | `feat(pulls): render and apply review suggestions` |

## What to build

- `fragments.LineComments` renders `SuggestionBody` as a diff-style block, plus `dark:prose-invert` on comment bodies.
- "Apply suggestion" for writers on an open PR, right side only (the existing `POST …/apply` rules); a 409 shows the
  branch-moved message.
- Migration (next free number after 117): `applied_at`, `applied_sha`, `applied_by` on `pull_line_comments`, set by
  `ApplySuggestion`. An applied suggestion shows a badge, "applied this in `sha`" and the updated line, and survives a
  reload.

## Acceptance criteria

- [x] A pending suggestion renders with Apply; applying creates one commit on the head branch and marks it applied
- [x] The applied state survives a reload and hides Apply
- [x] A left-side or closed-PR suggestion shows no Apply
- [x] Inline `code` is readable in dark mode
- [x] `mockups/pr_files.html` shows pending and applied; checked under `make mockups`
- [x] Verified at 1440 px in light and dark

## Blocked by

02
