# Unfinished settings surfaces

Created: 2026-11-12
Category: enhancement
Status: ready-for-human
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/unfinished-settings-surfaces` | `.worktrees/feat+unfinished-settings-surfaces` | `feat(ui): unfinished settings surfaces` | polish (wave 2 re-score) | L | Features +0.5, Content +0.1 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. Visible gaps in otherwise complete settings (Codex, Claude): organization teams and organization webhooks (the wave 2 polish PR reworded their empty states and removed the disabled buttons, nothing more); account export and additional email addresses ("Coming soon"); repository rename and organization name change; organization creation lists "Not available" and "Billing not yet supported" radios; the fork Owner defaults to an organization instead of the current user.
2. Teams are the largest: a forge with organizations is expected to group members and grant repositories to the group.

## Scope

- Needs a `/grill-with-docs` and `/to-spec` pass; split into one PR per surface.
- Teams: membership, repository grants, permission resolution (see `docs/access-control.md`). Org webhooks: reuse `docs/webhooks.md`.
- Rename is ticket 15; this ticket owns the rest. Remove any control whose feature is not shipping.

## Files

- `internal/view/pages/org_settings.templ`, `settings.templ`, `repo_settings.templ`, `new_organization.templ`, `repo_fork.templ`
- service and store layers per surface
- matching mocks

## Acceptance criteria

- [ ] Spec and tickets exist for teams, org webhooks, export and extra emails
- [ ] No disabled "not available" radios or buttons left in settings
- [ ] Fork owner defaults to the current user
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-human: teams is a feature that needs `/grill-with-docs` and `/to-spec`. The smaller gaps (export, extra emails, renames, org-creation radios, fork owner default) can split into an agent ticket during that session; repository rename is ticket 15.
