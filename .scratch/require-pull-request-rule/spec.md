# Protected branches accept direct pushes

Created: 2026-10-06
Category: enhancement
Status: needs-triage

## Problem

A branch protection rule's `require_review_count` and `require_status_checks` gate only PR merges (`BranchProtectionService.CheckMerge`). Push enforcement (`CheckPushCommand`) checks only `block_force_push`. A writer can therefore `git push origin main` straight past a rule that requires two approving reviews. Web edits, renames and deletes (`.scratch/web-file-edit/spec.md`) follow the same rules as a push, so they get through as well.

`docs/ROADMAP.md` (Phase 6.1) lists an `ErrPushRequiresPR` sentinel that was never built.

## Fix sketch

- Add a rule option (or treat `require_review_count > 0` as implying it) that refuses any direct update of a matching branch, whether by push or by web commit, so changes reach it only by merging a PR.
- Refuse the update per ref in receive-pack, with a message naming the rule, and refuse web commits with 422.

## Open questions

- Is this a new rule flag, or implied by required reviews and checks?
- May repo admins bypass it?

## Comments

**Malith Kuruppu, 2026-10-06:** Split out of web-file-edit triage.
