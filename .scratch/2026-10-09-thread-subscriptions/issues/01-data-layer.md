# Thread subscription table, store and service

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

## What

Migration 113 creates `thread_subscriptions` as in the spec, with a CHECK on `kind`, `state` and `reason`. Add `model.ThreadSubscription`, `store.ThreadSubscriptionStore` (wired into `Stores`) and `service.ThreadSubscriptionService` (wired into `Services`).

Store methods: `Upsert` (sets state and reason), `InsertIfAbsent`, `Get`, `ListByThread(state)`. Service methods: `Status(ctx, userID, repo, kind, number)` returning state and reason with the repo-watch fallback, `Set`, `AutoSubscribe` (insert-if-absent), `SubscribeOnMention` (upsert), `SubscribedUsers`, `MutedUsers`.

## Acceptance criteria

- [ ] Migration 113 applies cleanly; deleting a user or repo removes their rows.
- [ ] `AutoSubscribe` never changes an existing row; `Set` and `SubscribeOnMention` do.
- [ ] `Status` reports "subscribed (watching)" for a `watching` repo watch with no row, and "muted" wins over a repo watch.
- [ ] Store integration tests and service tests cover each method.
