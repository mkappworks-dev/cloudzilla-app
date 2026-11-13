# Push protection on HTTP and SSH

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 10
Parallel with: 11, 12, 16

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-push-protection` | `.worktrees/feat+phase-17.3-push-protection` | `feat(secrets): push protection (17.3)` | L |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

Daisy can push an AWS key today and nothing stops her. Both transports share one `vet` hook that runs after go-git stores the pack and before the ref moves: the place to refuse the push, with the pusher seeing only the one-line ref-status reason.

## Scope

- Export the added-commit walk: a `gittransport` function (for example `AddedCommits(s storer.Storer, tips []plumbing.Hash)`) built on `connectivity.added` / `addedOnto`, returning the commits the new values reach that no ref does. Do not use `forEachPushedCommit` for this (it walks a new branch's whole history).
- `PushScanService` (new, `internal/service/`): per push, on the first `vet` call, walk the added commits of every non-delete command, diff each commit's tree against its parents', and `secretscan.Scan` only added or changed blobs; memoise findings per commit; apply spec decisions 3, 6, 7 and 9 (closed alert skips, only high confidence blocks, `secret_scanning.push_protection`, the budget and fail-open); create or update the alert through `SecretAlertService.Record` with `reached_ref = false` and kind `blocked`.
- `service.PushVet` composing branch protection then the secret check; replace the inline `vet` closures in `internal/handler/git_http.go` and `internal/ssh/server.go` with it. The returned error's text is exactly `secret detected: <type> in <path>:<line>[ and N more]; if it is not a live secret, a writer can allow it at <base_url>/<owner>/<repo>/security/secrets/<number>`; every other error stays `internal error checking for secrets` with the cause logged.
- Post-receive: a `PushService.AfterPush` step (`secret_scan.post_receive`, under `afterPushTimeout`) scans applied commits that push protection did not finish or that bypassed it (generic findings, an incomplete scan, protection off), records `found` events, calls `MarkReached` for every applied commit, and calls `NotifySecretAlert` (ticket 11) for new alerts. `AfterWebCommit` reaches it too.
- Config `secret_scanning.{push_protection,max_blob_bytes,push_timeout,max_blobs}` (defaults `true`, 1 MiB, 10s, 5000) in `internal/config/config.go` with `validate()`; document in `docs/configuration.md`.
- Log line `push-protection: scan incomplete` with `repo_id`, reason and counts on any budget or scanner failure.

## Files (hotspots in bold)

- **`internal/gittransport/connectivity.go`**, `internal/gittransport/server.go` (only if the hook needs the pack's commit set)
- **`internal/handler/git_http.go`**, **`internal/ssh/server.go`**, `internal/service/push_vet.go` (new), `internal/service/push_scan_service.go` (new), **`internal/service/push_service.go`**
- `internal/config/config.go`, `docs/configuration.md`, **`internal/service/services.go`**

## Acceptance criteria

- [ ] Over HTTP and over SSH, a push adding `AKIA…` to a new branch is refused with the exact status text above and the ref does not move; a second ref sharing the commit is also refused, scanned once (assert one walk)
- [ ] A fast-forward adding a key, an annotated tag pointing at a commit adding one, and a force push are refused; a delete-only push, a push with no secret, and a push of a value with a closed alert pass
- [ ] A refused push creates an alert (`blocked`, `reached_ref = false`) and no notification; a retry after Allow (ticket 10 service call) passes and the location is appended
- [ ] A 1 ms `push_timeout`, a 1-blob cap and a failing scanner each let the push through, log `scan incomplete`, and the post-receive scan alerts with `incomplete = true`
- [ ] `secret_scanning.push_protection: false` lets a high-confidence push through and alerts after the push
- [ ] A repo with 5,000 changed files and a 100,000-commit history (see the large-repo harness) adds < 1 s to a small push; record the number in the PR
- [ ] Branch protection's refusals keep their exact status texts (existing tests unchanged)
- [ ] ~~Checked at desktop 1440 in light and dark~~ (no UI in this ticket)
- [ ] ~~The standing mocks in `mockups/` are updated~~ (no UI in this ticket)
- [ ] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [ ] `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Throwaway server (HTTP with a token and SSH): `git push` a branch with a key, with a fixture closed via the service, with a tag, with two refs sharing a commit; paste the exact `! [remote rejected]` lines from both transports into the PR.

## Comments

