# Webhooks

Webhooks let repo owners receive HTTP POST callbacks when events occur in a repository, and organization owners receive
them for every repository the organization owns.

## Supported Events

- `push` — fired when commits are pushed (HTTP or SSH receive-pack) or committed in the browser (new file, edit, rename,
  delete, profile README, applied suggestion)
- `issues` — fired on issue create, close, reopen, including closes by a closing keyword in a merged PR or a pushed
  commit (see [pr-merge](./pr-merge.md#closing-issues)), which fire on the issue's repo
- `pull_request` — fired on PR create, close, merge

## Delivery

Webhooks are dispatched fire-and-forget (`go s.Dispatch(...)`). Each delivery is recorded in `webhook_deliveries` with
the event name, payload, response code, and any error.

**HMAC signing:** if a secret is configured, requests include `X-Hub-Signature-256: sha256=<HMAC-SHA256>`
(GitHub-compatible).

**Retries:** a delivery that gets no response or a non-2xx (including a 3xx) is retried by the worker in
`cmd/server/main.go` after 1 min, 5 min, 30 min and 2 h, five attempts in all. Manual redelivery
(`POST .../hooks/{id}/redeliver?delivery_id=`) goes through the same path.

**Private networks:** every attempt — first delivery, retry and redelivery — dials through `dialPublic`
(`internal/service/netguard.go`, shared with repo import). It resolves the host itself, refuses if any address is
loopback, private, link-local, multicast, unspecified, reserved (`0.0.0.0/8`, `192.0.0.0/24`, `198.18.0.0/15`,
`240.0.0.0/4`), CGNAT (`100.64.0.0/10`), or an IPv6 form that can carry one of those (NAT64, 6to4, site-local), and
connects only to the addresses it vetted, so a DNS answer that changes after the check can't swap in a private one.
Redirects are not followed, and `HTTP_PROXY`/`HTTPS_PROXY` are ignored. A refused delivery is recorded as
`<host> resolves to a private network address`, never the address, and isn't retried; a DNS failure is recorded without
the resolver's address. `Create` runs the same check on the URL's current answer and also requires http or https and a
host; the API answers a refused URL with 400 and the reason. `webhook.allow_local_networks: true` turns both checks off,
for endpoints on your own network; it is separate from `import.allow_local_networks`. Webhooks created before this guard
that point at such an address (a Tailscale `100.x` address, say) now fail every delivery with that error until the
setting is on.

## Organization webhooks

A webhook has either a `repo_id` or an `org_id` (`webhooks_one_owner` CHECK); deleting the repository or the
organization cascades to it. An organization webhook receives the same events as a repository one from every repository
the organization owns, including ones created later, with the repository payloads unchanged. `Dispatch` finds them
through the repository's current `org_id` (`WebhookStore.ListForRepoEvents`), so a repository transferred out of the
organization stops firing its hooks and one transferred in starts, with no rows to move. Signing, retries, redelivery
and the private network guard are the repository path's.

Only an organization `owner` creates, lists, edits and deletes them, on the Webhooks section of `/orgs/{org}/settings`
or through `/api/orgs/{org}/hooks` (see below). A repository `admin` can't, and the repository settings page only says
"An organization webhook also fires for this repository", never the URL or secret. The repository endpoints can't reach
an organization hook: each checks `repo_id`.

## API Endpoints

All webhook endpoints are under `/api/repos/{owner}/{repo}/hooks`:

| Method | Path                                              | Auth         | Description                                                       |
| ------ | ------------------------------------------------- | ------------ | ----------------------------------------------------------------- |
| GET    | `/api/repos/{owner}/{repo}/hooks/`                | —            | List webhooks for repo                                            |
| POST   | `/api/repos/{owner}/{repo}/hooks/`                | Write access | Create webhook (`url`, `secret`, `events`); 400 for a refused URL |
| DELETE | `/api/repos/{owner}/{repo}/hooks/{id}`            | Write access | Delete webhook                                                    |
| GET    | `/api/repos/{owner}/{repo}/hooks/{id}/deliveries` | Write access | List delivery history                                             |

An organization's hooks are under `/api/orgs/{org}/hooks` with the same verbs (list, create, `PATCH` events, delete,
`/{id}/deliveries`, `/{id}/redeliver`), owner only; a token needs `repo:admin`. Creating one needs the same confirmation
as a repository webhook.

HTMX requests for create/delete swap `fragment-webhooks-list` into `#webhooks-list`.

Default events when `events` is omitted: `push,issues,pull_request`.

## WebhookService (`internal/service/webhook_service.go`)

- `Create(ctx, repoID, url, secret, events)` → `(*Webhook, error)`; a refused URL is a `*WebhookURLError` whose `Reason`
  is safe to show
- `ListByRepo(ctx, repoID)` → `([]Webhook, error)`
- `Delete(ctx, id, repoID)` → `error`
- `ListDeliveries(ctx, webhookID)` → `([]WebhookDelivery, error)`
- `CreateForOrg(ctx, orgID, url, secret, events)`, `ListByOrg`, `CountByOrg`, `DeleteForOrg(ctx, id, orgID)`,
  `ListDeliveriesForOrg`, `RedeliverForOrg`, `UpdateEventsForOrg` — the organization counterparts; the URL guard and
  `*WebhookURLError` are the same
- `Dispatch(repoID, event, payload)` — fire-and-forget; call as `go s.Webhook.Dispatch(...)`; also reaches the hooks of
  the repository's organization
- `PushPayload(repo, pusher, branch, headSHA)` → `map[string]any`
- `IssuePayload(action, repo, issue)` → `map[string]any`
- `PullPayload(action, repo, pr)` → `map[string]any`

## Repo Settings Page

`/{owner}/{repo}/settings` (write access required) — shows a **Collaborators** section, a **Webhooks** section, and (for
repo owners, and org owners on org repos) a **Transfer ownership** danger zone. The collaborators section is only
editable by the repo owner or an org owner (`CanManage`). Collaborators with `admin` role can see the settings page
(write access) but cannot modify collaborators or transfer.
