# Milestone 1 closing work

Created: 2026-10-09
Category: enhancement
Status: needs-triage

## Problem

Milestone 1 (Phases 0–15.3) is feature-complete. A coverage and review pass on 2026-10-08/09 raised coverage of `service`, `store`, `handler` and `backup` and turned up bugs, hardening gaps and doc drift. Part of the follow-up shipped; this folder tracks what is left.

## Already shipped

- #186 SSO service tests; fixed two panics on malformed LDAP and SAML input.
- #188 and #204 coverage rounds 1 and 2; fixed `ListPullsPaged` (18 columns scanned as 19) and `GET /api/user/keys` returning `null`.
- #202 require a SAML `AudienceRestriction`; release update maps a duplicate tag to `ErrReleaseTagInUse`; PRs created as drafts get `draft_at`.
- #205 project cards may not reference another repo's issue or pull request.
- #206 go1.27.2 and x/net v0.60.0 cleared 11 reachable `govulncheck` advisories; CI now runs `govulncheck`.
- #203 refreshed the README, ROADMAP and API reference.

## In flight

Branch `fix/handler-error-statuses` (not yet merged when this was written) fixes the handler status-code and validation bugs from the same pass: issue create validation, locked-discussion replies, the duplicate deploy-key message, `from-template` name conflicts, and 500-instead-of-404 on missing milestones, releases, issues, hooks and project columns. Check `git log origin/main` before touching those handlers.

## Not a bug

`AttentionService.ForUser` caps results at 20 only when the PR dependencies are unwired. Production always wires them (`services.go`), so the cap never applies there.

## Tickets

See `issues/`. Order of value: 01–03 and 06 are correctness and security; 04 and 05 are maintenance; 07 and 08 are longer-term.
