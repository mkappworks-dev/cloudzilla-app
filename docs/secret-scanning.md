# Secret scanning and push protection

Cloudzilla scans what is pushed for secrets, refuses a push that adds a high-confidence one, and keeps an alert list on
each repository's Security tab. A Cloudzilla access token found in a repository is deleted on the spot. Scanning is on
for every repository; there is no per-repo switch. Code: `internal/secretscan` (the scanner),
`internal/service/secret_*.go`, `push_scan_service.go`, `push_scan_web.go`, `push_vet.go`, and
`internal/handler/security_*.go`.

## Patterns and confidence

`secretscan.Scan(path, content, maxBytes)` returns at most 50 findings per blob, each with a type, a line and the value
(which never leaves memory). Content with a NUL in its first 8 KiB, and content over `maxBytes`, yields nothing. `path`
is reserved for per-path rules and unused today.

| Type                | Matches                                                                                                | Confidence |
| ------------------- | ------------------------------------------------------------------------------------------------------ | ---------- |
| `aws_access_key_id` | `AKIA` or `ASIA` plus 16 characters of `A-Z2-7`                                                        | high       |
| `private_key`       | a `-----BEGIN … PRIVATE KEY-----` header (RSA, EC, OpenSSH, PGP, plain), valued through its END marker | high       |
| `cloudzilla_token`  | `czp_` plus 64 hex; a finding only when its SHA-256 is in `access_tokens` (see below)                  | high       |
| `jwt_secret`        | `JWT_SECRET` / `jwt-secret` assignments with a literal of 16 or more characters                        | high       |
| `generic_api_key`   | `api_key` / `secret` / `token` assigned a quoted 20+ character literal of entropy ≥ 3.5                | low        |

Only high-confidence findings block a push. A low-confidence finding never blocks; it becomes an alert after the push. A
lower-confidence finding that repeats a value already found on the same line is dropped. The literal's value is rejected
as a placeholder when it contains `your`, `example`, `placeholder`, `changeme`, `xxxx`, `<` or `todo`.

`Mask` gives the hint stored with an alert: the first and last four characters (`AKIA…4WTB`), `****` for a value of 12
characters or fewer, and for a private key the key kind from its header, capped at 8 characters (`RSA`, `OPENSSH`). A
private key's value runs from its header through the `END` marker (at most 16 KiB; to the end of the blob if the marker
is missing), so two different keys of one type are two alerts and closing one allows only that key. The dedupe key is
`Hash(repoID, value)` = `sha256("<repo_id>:<value>")`.

## Alerts

One alert per (repository, secret hash). A second sighting of the same value adds a location (commit, path, line) and an
event to the existing alert, open or closed. Tables (migrations 133 to 135): `secret_scan_alerts`,
`secret_scan_locations`, `secret_scan_events`, then `repo_secret_scans` (migration 135, with a trigger that queues a row
for every new repository); migration 134 widens `notifications_type_check`.

An alert is `open` or `closed`. A closed alert carries a resolution: `revoked`, `false_positive` or `used_in_tests`.
Number, resolver and time are stored with it; `auto_revoked` marks the instance's own revoke and `incomplete` a finding
from a scan that ran out of budget. A **blocked** alert is derived, not stored: every location sits on a commit no ref
reaches yet (`reached_ref = false`). It lists under Open with a "Blocked push" badge and is not counted in the tab
badge. `SecretAlertService.SweepBlocked` deletes blocked-only alerts older than 7 days; once a ref reaches the value, it
is an ordinary open alert.

An alert's snippet is read from the stored commit at display time and masked server-side for everyone; there is no
reveal. If the commit is gone (a force push and `cz-admin gc`) the page says "commit no longer available" and the alert
and hint remain.

### Allow means close

A writer allows a blocked value by closing its alert as `false_positive` or `used_in_tests` (the **Allow** buttons on
the alert page; **Close as** also offers `revoked`). A value whose alert is closed, under any resolution, is never
blocked or re-alerted in that repository until the alert is reopened. There is no timer. A live Cloudzilla token cannot
be allowed: `allow` on it is `422`. Reopening blocks the value again only for commits not yet on the server: a commit a
ref already holds is not "added" by a later push, so it is not scanned.

## Push protection

Both transports build their `vet` hook with `PushVetService.Begin`, so branch protection and the secret check cannot
drift; see [git-transport](./git-transport.md#push-protection). The scan runs once per push, shared across refs: it
walks the commits the push adds (`gittransport.AddedCommits`), reads only the blobs each commit changed against its
parents, and memoises by commit, so two refs sharing a commit scan once and produce one alert. Branches and tags are
scanned, deletes are not.

A refused ref reads as one status line, over HTTP and SSH alike. go-git advertises no side-band, so there are no
`remote:` lines, and HTTP stays `200` and SSH exit `0`:

```
 ! [remote rejected] feat-aws -> feat-aws (secret detected: aws_access_key_id in config.env:1; if it is not a live secret, a writer can allow it at http://127.0.0.1:18093/siteadmin/secretlab/security/secrets/1)
```

With several findings, `:<line>` is followed by ` and N more`. A leaked Cloudzilla token reads
`secret detected: Cloudzilla access token in tok.env:1 (the token was revoked); see <url>`, and a failed alert write
reads `internal error checking for secrets` (the only fail-closed path: the match is already known). The URL is built
from `server.base_url`.

A closed alert met at check time only gains the location; the scan after the push adds the `found` event. An alert is
"scan incomplete" only for findings in commits the push-time scan did not reach.

A blocked push writes its alert at once, since the URL needs an id; the pusher's commit is on no ref, so the alert is
"blocked" until a ref reaches it.

### Fail open

The push-wide clock starts at the first ref. `secret_scanning.push_timeout` (10s), `secret_scanning.max_blobs` (5000) or
a scanner error stops the scan, logs `push-protection: scan incomplete`, lets the push through, and leaves the rest to
the post-receive scan, whose alerts say "scan incomplete". So does a failed read in the check (the closed-alert lookup,
the token lookup): it is logged and the post-receive scan settles the finding. A match always blocks; only a failed
write of the alert for a known match refuses the push. `secret_scanning.max_blob_bytes` (1 MiB) skips larger blobs on
the push path. With `secret_scanning.push_protection: false` nothing blocks and every finding is raised after the push.
Keys are in [configuration](./configuration.md).

### After the push

`PushScan.PostReceive`, started by `PushService.AfterPush`, scans what protection did not finish or what was allowed
through (low-confidence findings, an incomplete scan, protection off), writes `found` events, flips `reached_ref` on
every location whose commit now sits on an applied ref, notifies repository admins, and revokes any live token.

### Web editor

New file and edit run `PushScanService.CheckWebContent` on the new content before the commit: a blocking finding is a
`422` in the form's error with the same text and link and no commit. The alert has no commit to name, so it is blocked
until something reaches the value. A scan that cannot finish commits, and `AfterWebCommit` scans the commit afterwards.
A live Cloudzilla token in the content is deleted as on a push, its alert closes as `revoked`, and the `422` says the
token was revoked, with no allow link. A web-edit alert has no commit: the pages show its file and line and say the edit
was refused before it was committed. PR merges and squashes, wikis, issues and comments are not scanned: a merge adds
content already scanned on its head branch.

## Auto-revoke

A `czp_` finding is checked against `access_tokens` by `TokenRevoker.ResolveToken`:

| Outcome | What happens                                                                                                                   |
| ------- | ------------------------------------------------------------------------------------------------------------------------------ |
| unknown | not a finding: no alert, the push goes through                                                                                 |
| expired | no block; the alert opens closed as `revoked`, no user, without the "Auto-revoked" badge                                       |
| live    | token deleted; the alert closes as `revoked` with `auto_revoked` and no resolver; a blocked push is refused; the owner is told |

The audit entry `token.auto_revoke` is written by the system; a failed audit write never turns the revoke into a failed
push. OAuth-app tokens have no prefix and are not scanned for.

## Backfill

`SecretScanService.RunBackfill`, started in `cmd/server/main.go`, polls every 30 s, claims one `repo_secret_scans` row
that is `pending`, `failed` for an hour, or has an expired lease, and walks every ref's history once, reading each
distinct blob once, within `secret_scanning.backfill_timeout` (5m) and `backfill_max_blobs` (200,000); past either it
finishes as incomplete. It never blocks and its alerts carry the detail `history scan`. Migration 135 queues every
existing repository; a trigger queues new ones; mirror syncs and imports call `MarkPending` when they finish. The same
loop runs the blocked-alert sweep. Admins get one `secret_alert` digest per repository. The Security page shows
"Scanning existing history…" while a repository is not `done`.

The history walk skips blobs over `secret_scanning.max_blob_bytes`, like the push path. A `czp_` finding is settled as
on the push path: a live token is deleted and its alert closes as auto-revoked, an expired one files a closed `revoked`
alert, and an unknown one is dropped.

**A scan does not resume.** It is lease-based, but a scan that is interrupted (a restart, a lost lease) or stops at its
budget starts again from the first ref the next time; nothing records how far it got. Alerts already filed are not
duplicated (a known location is skipped), so the cost is time. A repository too large for one run finishes as
incomplete, with the same prefix of history each time; `secret_scanning.backfill_timeout` and
`secret_scanning.backfill_max_blobs` are the keys that bound a run, so raise them for a large repository.

## Who sees what

Everything on the Security tab (list, alert page, close, reopen, allow) needs `CanWrite`: the owner, an org owner, a
`writer` or `admin` collaborator, or a team with write. Snippets are masked for all of them. A reader or an anonymous
visitor gets the missing-repository `404` and sees no tab or count.

The actions are plain form `POST`s that redirect (`303`) to the alert page, not HTMX swaps:
`POST …/security/secrets/{n}/close` (`resolution`), `…/reopen`, `…/allow` (`resolution`). Routes are in
`internal/router/router.go`; handlers in `security_handler.go` and `security_page_handler.go`.

## Notifications and audit

`secret_alert` goes to repository admins (`CanManage`, including team grants) when a post-receive or backfill scan opens
an alert, and once more when a ref first reaches an alert that a blocked push or web edit created; a blocked push itself
notifies no one; `secret_allowed` goes to them when a writer allows a value, skipping the allower; `token_revoked` goes
to the token's owner. Messages carry the type and alert number, never the hint. See [notifications](./notifications.md).
Audit actions: `secret_scan.close`, `secret_scan.reopen`, `secret_scan.allow`, `token.auto_revoke`; metadata holds the
repository and secret type, never the value or its hint. There is no `secret_scanning_alert` [webhook](./webhooks.md)
event.

## Accepted costs

- **Guessable hashes.** `sha256(repo_id ":" value)` lets someone with database access confirm a guess of a low-entropy
  value; the hint already shows eight characters.
- **A refused push leaves objects.** go-git stores the pack before the hook runs, so the secret stays on disk as loose
  objects until `cz-admin gc` (14-day grace).
- **No side-band.** The pusher sees one status line, not a multi-line `remote:` block.
- **Fail open.** A slow or huge push is let through and alerted afterwards.
- **A closed alert silences the value** for the repository; reopen it to block again.

## What an end-to-end run showed

Native git against a throwaway server, with a token over HTTP and an ed25519 key over SSH. Refusal lines are exactly as
in the box above.

| Case                                                 | Outcome                                                                                          |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| AWS key on a new branch, HTTP and SSH                | refused, same line, alert `#1`; the second push added a location only                            |
| fast-forward on top of a clean branch                | refused, new alert                                                                               |
| lightweight tag, annotated tag (SSH)                 | refused, pointing at the alert of the commit                                                     |
| private key                                          | refused: `private_key in id_rsa:1`                                                               |
| two refs, one commit, one push                       | both refused, one alert                                                                          |
| live `czp_` token                                    | refused with "(the token was revoked)"; token row deleted; alert closed `revoked`                |
| unknown `czp_` value                                 | accepted, no alert                                                                               |
| expired `czp_` token                                 | accepted; alert opened closed `revoked`, no "Auto-revoked" badge                                 |
| Allow as `used_in_tests`, then push again            | accepted, over HTTP and SSH                                                                      |
| Allow on a live-token alert, or `resolution=revoked` | `422`                                                                                            |
| reopen, push the already-pushed commit to a new name | accepted (nothing added)                                                                         |
| reopen, push a new commit with the same key          | refused                                                                                          |
| web editor, new file and edit with a key             | `422` with the line; no commit; clean file `303`                                                 |
| history backfill of a repository                     | AWS key, private key on another branch and a live token found; token deleted                     |
| `push_protection: false`                             | key accepted over both transports, alert after the push; live token still deleted after the push |
