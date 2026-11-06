# Draft toggles

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-draft-toggles` | `.worktrees/feat+pr-draft-toggles` | `feat(pulls): convert to draft and ready for review from the PR page` |

## What to build

- A muted "Draft" merge box with Ready for review (`PATCH is_draft=false`) replaces the hidden box for a drafted PR.
- "Convert to draft" at the bottom of the sidebar for writers and the author on an open PR (`PATCH is_draft=true`).
- No Review prompt or button while drafted.

## Acceptance criteria

- [ ] Both toggles work for a writer and for the author; a reader sees neither
- [ ] A drafted PR shows the Draft badge, banner and muted box, with no Merge button
- [ ] `docs/pr-merge.md` "Draft Pull Requests" matches the UI
- [ ] `mockups/pr.html` shows the draft states; checked under `make mockups`
- [ ] Verified at 1440 px in light and dark

## Blocked by

03
