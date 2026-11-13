# Secret scanning and push protection (ROADMAP 17.3)

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent

Mock: [README.md](./README.md), [security.html](./security.html) and [alert.html](./alert.html). Parent ticket:
`../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`. Signing's spec ([spec.md](./spec.md)) is done and shares
this folder; its "For ticket 17" section lists what this spec inherits.

## Problem

Nothing stops a key from being committed. Daisy pastes `AWS_ACCESS_KEY_ID=AKIA…7QXZ` into `config/deploy.env`, pushes,
and the key is in history and in every clone. ROADMAP 17.3 plans a post-push regex scan and an alert list, with no
UI, no way to stop the push, and one `state` column that cannot say why an alert was closed.

After this spec the push is refused with the alert's URL; a writer who knows the value is a fixture closes the alert as
"used in tests" and Daisy pushes again. A Cloudzilla token found in a push is deleted on the spot and its owner told.

## What exists today (read, not assumed)

- **One push path, two transports.** `gittransport.NewServer(storer, vet)` serves receive-pack for HTTP
  (`internal/handler/git_http.go`) and SSH (`internal/ssh/server.go`). Each builds a `vet` closure that calls
  `BranchProtectionService.CheckPushCommand`. `casStorer.move` calls `vet` once per ref, after go-git has stored the
  pack and before `gitref.Move`; a returned error becomes that ref's status line and the rest of the push applies.
- **The only text a pusher gets is that error.** go-git's receive-pack advertises `ofs-delta`, `delete-refs` and
  `report-status` only (`plumbing/transport/server/server.go`, `setSupportedCapabilities`): no side-band, so no
  `remote:` lines. A refused ref reads `! [remote rejected] main -> main (<err.Error()>)`. Over HTTP the response is
  still 200, over SSH exit 0.
- **A refused push leaves its objects.** go-git stores the pack first; unreferenced loose objects wait for
  `cz-admin gc` (grace 14 days). So a secret in a refused push is on the server's disk until then.
- **The added-commit walk exists, privately.** `connectivity.added(tip)` (`internal/gittransport/connectivity.go`) returns
  the commits a value reaches that no ref does, as `git rev-list <new> --not --all`. `forEachPushedCommit`
  (`repo_service_push.go`) is **not** that: for a `Create` it logs the branch's whole history.
- **After a push.** `PushService.AfterPush` starts webhooks, activity, issue closing, `OnPostReceive`, indexing and the
  dependency parse in background goroutines, each under `afterPushTimeout` (5 min). `AfterWebCommit` feeds it a web
  commit's ref update; PR merges and wiki commits do not call it.
- **Cloudzilla tokens.** `czp_` + 64 hex (`newToken`); only the SHA-256 is stored (`access_tokens.token_hash`);
  `AccessTokenStore.GetByHash` and `Delete(tokenID, userID)` exist. OAuth-app tokens have no prefix and are out of scope.
- **Notifications.** `model.Notification` types are a closed set enforced by the `notifications_type_check` constraint
  (last redefined in migration 099); `NotifyRepoTransfer` is the nearest pattern. There is no repo-admin recipient lookup
  yet. Roles: `CanWrite` (owner, org owner, `writer`/`admin`), `CanManage` (owner, org owner, `admin`), plus team grants
  from #326; see [access-control](../../docs/access-control.md).
- **Background workers.** `MirrorService.Run` polls, claims due rows with a lease (`ClaimDue`) and runs at most
  `mirror.max_concurrent` at a time; the template for a resumable backfill.
- **Config.** `server.base_url` (default `http://localhost:8080`) builds the alert URL; config structs live in
  `internal/config/config.go` with a `validate()`.
- **Mocks.** `shell.js`'s `REPO_TABS` has no Security tab; the standing mocks live in `mockups/` and the scratch mocks
  (`security.html`, `alert.html`) beside this spec.

## Decisions

Settled with the user on 2026-11-13, except those marked *derived*.

1. **Alert state is `open`/`closed` plus a `resolution`** (`revoked`, `false_positive`, `used_in_tests`), `resolved_by`,
   `resolved_at`, and locations in their own table. Not ROADMAP's `open|resolved|false_positive`.
2. **Push protection rejects the push** (HTTP and SSH) before the ref moves. The message is **one line**: the secret
   type, `file:line`, and the alert URL, in the ref status. Side-band (the mock's multi-line `remote:` block) is out of
   scope; the mock's terminal pane is redrawn to the one line.
3. **Allow = close.** Any writer (`CanWrite`, the pusher included) may allow a blocked alert by closing it as
   `false_positive` or `used_in_tests`. A value whose alert is closed (any resolution) is never blocked or re-alerted
   in that repository until the alert is reopened. This **replaces the mock's one-hour allowance** (and the first
   answer given on the same day), which would block Daisy's fixture again on her next branch.
4. **One alert per (repo, secret hash).** Another sighting adds a location (and an event) to the alert, open or closed.
   The hash is `sha256(repo_id ":" value)`; low-entropy values are guessable from it, a cost accepted because the
   hint already shows eight characters.
5. **Cloudzilla tokens are revoked on any live match** (a blocked push or a scan): the token is deleted, the alert
   closes as `revoked` with `resolved_by` NULL and the "Auto-revoked" badge, and the token owner gets a notification and
   mail. A live token cannot be allowed. A `czp_` value that matches no stored hash is not a finding.
6. **Static pattern set, no custom patterns.** Each pattern has a `confidence`. Only `high` patterns block a push; the
   generic API key pattern alerts after the push and never blocks. Custom patterns and path ignores are follow-ups.
7. **On for every repository, no per-repo switch.** One operator switch, `secret_scanning.push_protection` (default
   `true`), turns off blocking only; scanning always runs.
8. **Existing history is scanned once**: a lease-based background job per repository over all refs, resumable, with a
   per-repo time and size budget. It never blocks; its alerts notify admins in one digest per repo. Pull mirrors and
   imports are backfilled after each sync or import (alerts only).
9. **Push path budget, fail open.** Scan only blobs the push adds (the `connectivity.added` walk), skipping binary blobs
   and blobs over `secret_scanning.max_blob_bytes` (1 MiB); once per push, shared across refs. A 10 s wall clock
   (`secret_scanning.push_timeout`), a 5,000-blob cap or a scanner error lets the push through, logged, with a
   post-receive scan that creates alerts; an alert from an incomplete scan says "scan incomplete". A match always blocks.
10. **What is scanned**: pushes to branches and tags (not deletes), the web file editor and delete (rejected with a form
    error carrying the same alert link). Not scanned: PR merges and squashes (their content was scanned on the head
    branch), wikis, issues and PR comments.
11. **Access**: the Security tab, alert list and alert page, close, reopen and allow all need `CanWrite`. Readers and
    anonymous visitors get the missing-repo 404 and see no tab or count. The snippet is read from the commit and masked
    server-side for everyone; there is no reveal.
12. **Notifications**: repo admins (`CanManage` users, including through team grants) are notified when a post-receive
    or backfill scan opens an alert and when a writer allows a value; not for a blocked push. The token owner is told of
    an auto-revoke. No new webhook event. Audit actions: `secret_scan.close`, `secret_scan.reopen`,
    `secret_scan.allow`, `token.auto_revoke`.
13. *Derived:* a blocked push creates its alert at once (the URL needs an id). An alert whose every location is on a
    commit no ref reaches is `blocked` ("Blocked push" badge, listed under Open, not counted in the tab badge) and is
    deleted by the backfill worker's sweep after 7 days if no ref ever reaches it. If the pusher fixes the commit, the
    sweep removes it; if a ref later reaches the value, the alert is an ordinary open one.
14. *Derived:* an alert's snippet is read from the stored commit and blob at display time. If the commit is gone (force
    push plus `cz-admin gc`) the page says "commit no longer available"; the alert and hint remain.
15. *Derived:* the scan reads the stored tree, so the instance signature on server-made commits (ADR 0002) changes
    nothing: it adds a header, not blob content.
16. *Derived:* an auto-revoke inside a blocked push still refuses the push; the pusher is told the token was revoked
    ("a Cloudzilla token was found and revoked").

## Design

### Schema

Re-check the next free number on `origin/main` when a ticket starts (131 is the highest as of 79f4ef23). Ticket 10 takes
the next number; the later migrations (11, 16) take the next free ones in order of merge.

```sql
-- 132_create_secret_scan_alerts.sql
CREATE TABLE secret_scan_alerts (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id        BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number         INTEGER NOT NULL,                 -- per-repo, the "#12" in the URL
    secret_type    TEXT NOT NULL,                    -- pattern id: 'aws_access_key_id', 'private_key', 'cloudzilla_token'…
    secret_hash    TEXT NOT NULL,                    -- sha256(repo_id ":" value), hex
    secret_hint    TEXT NOT NULL,                    -- first/last 4 characters, or the PEM header line
    state          TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','closed')),
    resolution     TEXT CHECK (resolution IN ('revoked','false_positive','used_in_tests')),
    resolved_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    resolved_at    TIMESTAMPTZ,
    auto_revoked   BOOLEAN NOT NULL DEFAULT FALSE,
    incomplete     BOOLEAN NOT NULL DEFAULT FALSE,   -- found by a scan that hit its budget
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((state = 'closed') = (resolution IS NOT NULL)),
    UNIQUE (repo_id, number),
    UNIQUE (repo_id, secret_hash)
);
CREATE INDEX idx_secret_scan_alerts_repo_state ON secret_scan_alerts(repo_id, state);

CREATE TABLE secret_scan_locations (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_id    BIGINT NOT NULL REFERENCES secret_scan_alerts(id) ON DELETE CASCADE,
    commit_sha  TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    reached_ref BOOLEAN NOT NULL,                    -- false while only a blocked push held the commit
    UNIQUE (alert_id, commit_sha, file_path, line_number)
);

CREATE TABLE secret_scan_events (                   -- the alert page's Activity timeline
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_id   BIGINT NOT NULL REFERENCES secret_scan_alerts(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('found','blocked','closed','reopened','allowed','auto_revoked')),
    actor_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ref        TEXT NOT NULL DEFAULT '',
    commit_sha TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',             -- resolution, or 'history scan'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 133_secret_scan_notifications.sql: widen notifications_type_check with 'secret_alert', 'secret_allowed',
--   'token_revoked' (re-read the latest definition, migration 099, when building)

-- 134_create_repo_secret_scans.sql
CREATE TABLE repo_secret_scans (                    -- backfill job state, one row per repository
    repo_id      BIGINT PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    state        TEXT NOT NULL CHECK (state IN ('pending','running','done','failed')),
    incomplete   BOOLEAN NOT NULL DEFAULT FALSE,
    lease_until  TIMESTAMPTZ,
    scanned_at   TIMESTAMPTZ,
    error        TEXT NOT NULL DEFAULT ''
);
```

Migration 134 inserts a `pending` row for every existing repository; new repositories get one at creation. A repo whose
row is not `done` shows "Scanning existing history…" on the Security page.

### Scanner (`internal/secretscan`)

A pure package, no database. `Scan(path string, content []byte) []Finding` returns `Finding{Type, Line, Value, Confidence}`
from a static `[]Pattern`; `Mask(value)` gives the hint; `Hash(repoID, value)` the dedupe key. `Value` stays in memory
only. The initial set, each a regexp with a fixture and a near-miss test:

| Type                | Matches                                                    | Confidence |
| ------------------- | ---------------------------------------------------------- | ---------- |
| `aws_access_key_id` | `AKIA`/`ASIA` + 16 base32 characters                       | high       |
| `private_key`       | a `-----BEGIN … PRIVATE KEY-----` header (RSA, EC, OpenSSH, PGP, plain) | high |
| `cloudzilla_token`  | `czp_` + 64 hex                                            | high, and only a finding if the hash is in `access_tokens` |
| `jwt_secret`        | `JWT_SECRET`/`jwt_secret` style assignments with a literal ≥ 16 characters | high |
| `generic_api_key`   | `api[_-]?key`/`secret`/`token` assignments with a long high-entropy literal | low (alert only) |

Skipped before matching: binary content (a NUL byte in the first 8 KiB) and blobs over `max_blob_bytes`. Findings
inside one blob cap at 50.

### Push path

`PushScanService` owns the scan and the alert writes; the two transports' `vet` closures call one shared
`service.PushVet(...)` that runs branch protection first, then the secret check, so HTTP and SSH cannot drift.

1. Per push (not per ref): on the first `vet` call, build the set of added commits from the new values of every
   non-delete command, using an exported form of `connectivity.added`; `forEachPushedCommit` is not used (it walks all
   of a new branch's history). For each added commit, diff its tree against its parents' (the same per-commit tree
   compare `checkTree` does) and scan only added or changed blobs. Memoise the findings by commit, so refs that share
   commits scan once.
2. For each finding: look up `(repo_id, hash)`. A closed alert → skip (decision 3), but append the location. An open
   alert or none → it blocks if `confidence == high` and `secret_scanning.push_protection`. A `cloudzilla_token` finding
   is checked against `access_tokens` first; a miss is dropped, a hit is revoked (ticket 14) and blocks.
3. A blocking finding creates or updates the alert (`blocked` location, `reached_ref = false`, event `blocked`) inside
   `vet`, and returns a sentinel error whose text is `secret detected: <type> in <path>:<line>[ and N more]; if it is not
   a live secret, a writer can allow it at <base_url>/<owner>/<repo>/security/secrets/<number>`. Only that text goes to
   the pusher; DB errors are logged and the push is refused as `internal error checking for secrets` (fail closed only
   for a failed alert write, since a match is already known).
4. Budget: the push-wide clock starts at the first `vet` call. Exceeding `push_timeout`, `max_blobs` or hitting a scanner
   error stops the scan, logs `push-protection: scan incomplete`, allows the ref, and marks the push for the
   post-receive scan.
5. Post-receive (`PushService.AfterPush`, a new background step under `afterPushTimeout`): scan the applied commits that
   push protection did not finish or that were allowed through (generic findings, an incomplete scan, protection off),
   write `found` events, flip `reached_ref` to true for every location whose commit now sits on an applied ref, open
   notifications (ticket 11) and revoke any live token (ticket 14).

Web editor (`CommitFile`, `editFile`, `DeleteFile`): the handler scans the new content before the commit; a blocking
finding returns 422 with the same text and link, no commit is made, and the alert is created as for a push.

### Backfill

`SecretScanService.RunBackfill(ctx)` is started from `cmd/server/main.go` beside the mirror scheduler: poll every 30 s,
claim a repo whose `repo_secret_scans` row is `pending`, `failed` (after 1 h) or has an expired lease, scan every
reachable blob once (a visited set of blob hashes, so a file unchanged across 10,000 commits is read once) within
`secret_scanning.backfill_timeout` (5 min) and `backfill_max_blobs` (200,000), and mark `done` or `incomplete`. Mirror
syncs and imports call `MarkPending(repoID)` when they finish. The same loop deletes `blocked`-only alerts older than 7
days.

### UI

- **Security tab** (`internal/view/fragments/repo_subnav.templ`): after Releases, before Settings, shown only when the
  viewer `CanWrite`; its badge is the open (non-blocked) count.
- **`GET /{owner}/{repo}/security/secrets`**: Open/Closed tabs with counts, a type filter, rows (type, masked hint,
  badges Auto-revoked / Blocked push / the resolution, `#n`, `file:line`, commit, pusher, age), the empty state, and
  "Scanning existing history…" while the backfill runs.
- **`GET …/security/secrets/{n}`**: the mock's alert page: masked snippet, How to fix, Activity, the aside (pattern, all
  locations, whether the value is on the default branch), **Close as** (Revoked, False positive, Used in tests), Reopen,
  and for a `blocked` alert the banner with **Allow as used in tests / false positive**.
- **Actions** (writers, HTMX variants as elsewhere): `POST …/{n}/close` (`resolution`), `…/reopen`, `…/allow`
  (`resolution` ∈ `false_positive`, `used_in_tests`). `allow` on a live-token alert is refused.
- **Copy to change in the mocks, done in this PR:** `alert.html`'s blocked banner (no one-hour allowance) and
  `security.html`'s `push` terminal (one line). Ticket 12 also adds the Security tab to `mockups/shell.js` and a standing
  `mockups/security.html` and `mockups/security_alert.html`.

## Slicing

| Ticket | Slice | Blocked by | Parallel with |
| ------ | ----- | ---------- | ------------- |
| [09](./issues/09-secret-scanner.md) | `internal/secretscan`: patterns, masking, hashing, caps | — | — |
| [10](./issues/10-secret-scan-alerts.md) | Alert/location/event tables, store, `SecretAlertService` (dedupe, close, reopen, allow), model | 09 | — |
| [11](./issues/11-secret-scan-notifications.md) | Admin and token-owner notifications, audit actions, notification types | 10 | 12, 13 |
| [12](./issues/12-security-tab-ui.md) | Security tab, alert list and page, actions, mocks | 10 | 11, 13 |
| [13](./issues/13-push-protection.md) | Push protection on HTTP and SSH, post-receive scan, budget, config | 10 | 11, 12, 16 |
| [14](./issues/14-auto-revoke-tokens.md) | Auto-revoke Cloudzilla tokens | 11, 12, 13 | 15, 16 |
| [15](./issues/15-web-editor-scanning.md) | Scan and reject web editor commits | 13 | 14, 16 |
| [16](./issues/16-history-backfill.md) | Backfill job, mirror/import hooks, blocked-alert sweep, scan-state banner | 10, 11, 12 | 13, 14, 15 |
| [17](./issues/17-secret-scanning-docs-and-e2e.md) | Docs, ROADMAP rewrite, native-git end-to-end on a throwaway server | 09–16 | — |

Shared-file conflicts: 12 and 16 both edit the Security page view model (16 waits for 12); 13, 14 and 15 all edit `PushScanService` (14 and 15 wait for 13); 11 and 16 both add a migration and
`NotificationService` methods. Migrations 132–134 collide with anything landing first: re-check at build. Signing
follow-ups (`tech/signing-key-revocation`, `tech/signing-key-squatting`) may touch signing code; nothing here depends on
them.

## Testing

Unit tests per ticket. The scanner is table-driven: every pattern's hit, near-miss and false-positive fixture, binary
and oversize skips, the 50-finding cap. The push path needs real packs: `internal/gittransport/server_test.go` and
`internal/ssh/server_test.go` helpers push through both transports and assert the exact ref-status text over each. Cases:
a key in a new branch, in a fast-forward, in a second ref sharing the commit (scanned once), in a tag; a closed alert
not blocking; a reopened one blocking; a live and a dead `czp_` token; a timeout and a blob cap failing open and the
post-receive scan alerting; a delete-only push; protection off. See [testing](../../docs/testing.md).

## Verification

Every UI ticket is checked on a throwaway server: scratch Postgres on a free port, a unique `CZ_AUTH_COOKIE_NAME` per
`127.0.0.1` server, signed in as `admin@example.test` / `cloudzilla-seed`, at desktop 1440 in **light and dark** (set
`cz-theme`; emulate dark before the first check). The standing mocks in `mockups/` change in the same PR as the page and
are checked under `make mockups` in both themes. Pushes for the checks come from native git against the throwaway server,
over HTTP with a token and over SSH.

## Out of scope

- Custom patterns, path ignores, a dry run, validity checks against issuers.
- Side-band output (`remote:` lines) and a delegated bypass-request flow.
- A `secret_scanning_alert` webhook event; an alerts JSON API.
- Scanning wikis, issues, PR comments, PR merges; OAuth-app tokens as a pattern.
- Rewriting history or purging a leaked blob; per-repo enable switches.

## Comments

Claude, 2026-11-13: forge comparison behind the decisions. GitHub scans full history, lets a pusher bypass for 3 hours
with a reason and fails open on timeout; GitLab's push protection uses high-confidence patterns only, skips blobs over
1 MiB, fails open on timeout and auto-revokes only on public projects; Gitea/Forgejo have none, and Bitbucket Data Center
detects without blocking. Decision 3 departs from the mock's one-hour allowance on purpose.
