# Organization webhooks

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —
Parallel: safe with 01, 02, 03. Shares `org_settings.templ` with 05 and 07, which wait for this.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/organization-webhooks` | `.worktrees/feat+organization-webhooks` | `feat(orgs): organization-wide webhooks` |

## What

Build the Organization Webhooks section per the [spec](../spec.md#organization-webhooks). Read `docs/webhooks.md`.

- Migration: `webhooks.repo_id` nullable, add nullable `org_id` (FK, cascade), CHECK exactly one set.
- `WebhookStore`/`WebhookService`: `Create/List/Delete` by org (same URL guard, `*WebhookURLError`), and `Dispatch`
  also loads hooks of the repository's Organization. Delivery, retry and redelivery code does not change.
- API `/api/orgs/{org}/hooks` (list, create, delete, deliveries), Organization `owner` only, `repo:admin` token scope
  and `TargetAllows` as for `/api/orgs/{org}/…`. Update `docs/webhooks.md` and the `docs/api-reference.md`.
- `org_settings.templ`: replace the empty state with the list, add form and deliveries link, reusing
  `fragment-webhooks-list`.
- Repository settings: a read-only line "An organization webhook also fires for this repository" (no URL or secret).
- Repository transferred out of or into the Organization changes which hooks fire; no extra rows to move.

## Tests

Service: a push, issue and PR in two org repos dispatch to the org hook; a repo of another Organization does not; a
transferred-out repo stops. Handler: a `member` and a repository `admin` get 403; `owner` creates; refused URL gives
400. Store: the CHECK rejects both-set and neither-set.

## Mocks

`mockups/org_settings.html` Webhooks section (empty, list). Repository settings has no standing mock: note the new line
in `mockups/COVERAGE.md`. Light and dark at 1440.

## Acceptance criteria

- [x] An org hook receives events from every org repo, signed, with retries
- [x] Only owners manage it; repo settings shows the read-only note
- [x] Docs and mock updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Point a hook at a local listener (set `webhook.allow_local_networks`), push to two org repos.

## Comments
