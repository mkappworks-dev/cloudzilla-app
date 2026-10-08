# Refuse direct pushes and branch deletes

Created: 2026-10-08
Category: enhancement
Status: done
Blocked by: 01

A push that creates, updates or deletes a branch matching a rule with `require_pull_request` is refused per ref over HTTP and SSH, naming the rule.

## Acceptance criteria

- [x] `CheckPushCommand` returns `ErrPushRequiresPR`, wrapped with the rule's pattern, for a create, fast-forward, force push or delete, without walking history.
- [x] It passes through `CheckPushCommand` unchanged (not mapped to `ErrProtectionCheckFailed`), so the pusher sees `pull request required by branch protection: rule "main"`.
- [x] Other refs in the same push still apply; tags and unmatched branches are untouched.
- [x] `CheckDelete` and `DELETE /api/repos/{owner}/{repo}/branches` refuse with 422, and so does `POST` of a branch name the rule matches (a pushed create is refused too).
- [x] A merge through `UpdatePull` still lands on a flagged branch.
- [x] `docs/git-transport.md` and `docs/api-reference.md` describe the refusal.
