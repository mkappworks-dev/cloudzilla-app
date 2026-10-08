# Extract the post-receive block into PushService

Created: 2026-10-08
Category: bug
Status: done
Parent: ../spec.md

## What to build

One service method that runs everything a push does after refs are applied, taking the repo, an actor and the applied ref commands. `internal/handler/git_http.go` and `internal/ssh/server.go` call it in place of their duplicated blocks.

## Acceptance criteria

- [x] `PushService.AfterPush(repo, gitRepo, actor, commands)` dispatches push webhooks per updated branch, records push activity, closes issues, runs `OnPostReceive`, re-indexes and parses dependencies, each in its own `concurrency.Go`.
- [x] A zero actor (deploy-key push) skips the activity event and the issue closer, as before.
- [x] HTTP and SSH call it and keep no copy of the block.
- [x] A unit test covers the webhook, the activity event and the no-actor case.

## Blocked by

None.
