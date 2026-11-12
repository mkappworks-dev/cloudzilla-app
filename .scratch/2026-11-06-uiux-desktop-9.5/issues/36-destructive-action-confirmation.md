# Destructive actions: prominence and confirmation

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 31

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/destructive-action-confirmation` | `.worktrees/fix+destructive-action-confirmation` | `fix(ui): destructive actions: prominence and confirmation` | polish (wave 2 re-score) | M | Interaction +0.3, Visual +0.1 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. Row-level destructive actions are loud: a solid red Delete on every project row, a red-outline Delete on every branch and tag (Claude, `repo-settings`, `admin-user`).
2. Archive and Delete repository need only the password, with no type-the-name confirmation; the confirmation inputs have no real labels.
3. Repository visibility is a plain toggle among the feature toggles with a separate Save, so going private looks as light as enabling the wiki (Claude, `repo-settings`).

## Scope

- Make row-level destructive actions secondary (icon or overflow menu). Require typing the repository name for delete, archive and visibility change, and label the inputs.
- Move Visibility to its own danger-style row with a confirmation dialog.
- Overlaps ticket 31 for the project board delete button: do that one here and tick it there.

## Files

- `internal/view/pages/repo_settings.templ`, `projects.templ`, `admin_user.templ`
- `components/confirm_dialog.templ`
- matching mocks

## Acceptance criteria

- [ ] No filled destructive buttons on list rows
- [ ] Delete, archive and visibility change need the repository name typed
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Takes over the project-row Delete from 31, so it waits for 31.
