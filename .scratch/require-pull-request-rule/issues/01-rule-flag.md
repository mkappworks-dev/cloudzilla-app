# Rule flag: require_pull_request

Created: 2026-10-08
Category: enhancement
Status: done
Blocked by: none

Branch protection rules get a `require_pull_request` boolean, default off, that can be set from the settings dialog and the API.

## Acceptance criteria

- [x] Migration 108 adds `branch_protections.require_pull_request BOOLEAN NOT NULL DEFAULT FALSE`; existing rules keep their behaviour.
- [x] `model.BranchProtection.RequirePullRequest` (`require_pull_request` in JSON) is read and written by every `BranchProtectionStore` query.
- [x] `BranchProtectionService.Update` takes the flag; `POST` and `PATCH .../branches/protections` accept `require_pull_request=true`.
- [x] The Add rule dialog has a "Require a pull request" checkbox, and a rule's row shows a badge when it is set.
- [x] `ErrPushRequiresPR` exists in the service package.
- [x] A store/service test round-trips the flag; `docs/api-reference.md` lists the field.
