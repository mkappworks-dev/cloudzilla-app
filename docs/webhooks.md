# Webhooks

Webhooks let repo owners receive HTTP POST callbacks when events occur in a repository.

## Supported Events

- `push` — fired when commits are pushed (HTTP or SSH receive-pack)
- `issues` — fired on issue create, close, reopen
- `pull_request` — fired on PR create, close, merge

## Delivery

Webhooks are dispatched fire-and-forget (`go s.Dispatch(...)`). Each delivery is recorded in `webhook_deliveries` with the event name, payload, response code, and any error.

**HMAC signing:** if a secret is configured, requests include `X-Hub-Signature-256: sha256=<HMAC-SHA256>` (GitHub-compatible).

## API Endpoints

All webhook endpoints are under `/api/repos/{owner}/{repo}/hooks`:

| Method | Path                                              | Auth         | Description                                |
| ------ | ------------------------------------------------- | ------------ | ------------------------------------------ |
| GET    | `/api/repos/{owner}/{repo}/hooks/`                | —            | List webhooks for repo                     |
| POST   | `/api/repos/{owner}/{repo}/hooks/`                | Write access | Create webhook (`url`, `secret`, `events`) |
| DELETE | `/api/repos/{owner}/{repo}/hooks/{id}`            | Write access | Delete webhook                             |
| GET    | `/api/repos/{owner}/{repo}/hooks/{id}/deliveries` | Write access | List delivery history                      |

HTMX requests for create/delete swap `fragment-webhooks-list` into `#webhooks-list`.

Default events when `events` is omitted: `push,issues,pull_request`.

## WebhookService (`internal/service/webhook_service.go`)

- `Create(ctx, repoID, url, secret, events)` → `(*Webhook, error)`
- `ListByRepo(ctx, repoID)` → `([]Webhook, error)`
- `Delete(ctx, id, repoID)` → `error`
- `ListDeliveries(ctx, webhookID)` → `([]WebhookDelivery, error)`
- `Dispatch(repoID, event, payload)` — fire-and-forget; call as `go s.Webhook.Dispatch(...)`
- `PushPayload(repo, pusher, branch, headSHA)` → `map[string]any`
- `IssuePayload(action, repo, issue)` → `map[string]any`
- `PullPayload(action, repo, pr)` → `map[string]any`

## Repo Settings Page

`/{owner}/{repo}/settings` (write access required) — shows a **Collaborators** section, a **Webhooks** section, and (for personal repo owners) a **Transfer Ownership** danger zone. The collaborators section is only editable by the repo owner or an org owner (`CanManage`). Collaborators with `admin` role can see the settings page (write access) but cannot modify collaborators or transfer.
