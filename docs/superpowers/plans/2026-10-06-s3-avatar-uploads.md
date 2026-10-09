# S3 Avatar Uploads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users and org owners can upload, replace and remove an avatar. The image is stored on a local directory or an S3-compatible bucket, served from `/avatars/*` with immutable caching, and shown wherever an account avatar appears.

**Architecture:**
- A leaf package `internal/storage` defines a `Backend` interface with local and S3 implementations.
- A pure package `internal/avatar` validates and re-encodes images.
- `AvatarService` orders the object writes against the `avatar_key` column swap.
- Handlers upload, remove and serve.
- One `components.Avatar` takes a URL and falls back to initials.

**Tech Stack:** Go, chi v5, aws-sdk-go-v2 (`config`, `credentials`, `service/s3`), `golang.org/x/image` (WebP, CatmullRom), PostgreSQL, Templ, htmx.

**Spec:** `.scratch/2026-10-06-phase-20.1-s3-avatar-uploads/spec.md`. **Tickets:** `.scratch/2026-10-06-phase-20.1-s3-avatar-uploads/issues/01–05`.

## Global Constraints

- Work on the session branch. Integration tests need `TEST_DATABASE_DSN` (migrated). Without it, DB tests skip silently.
- Edit `.templ` files, then run `make generate-templ`. Never hand-edit `*_templ.go`, and don't run `templ fmt`.
- Comments only for a *why* the code can't show, one line by default.
- Commit with `git add <exact paths>`. Never stage `.claude/`.
- Before each commit:
  - `go build ./...`
  - `go vet ./...`
  - `golangci-lint run ./...` when it is installed
  - the touched packages' tests with the DSN set
- The migration takes the next free number at commit time, after checking `git log HEAD..origin/main`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/storage/storage.go` (new) | `Backend`, `ErrNotFound`, `ValidKey`, `New` |
| `internal/storage/local.go` (new) | `LocalBackend` over `os.Root` |
| `internal/storage/s3.go` (new) | `S3Backend` over aws-sdk-go-v2 |
| `internal/storage/storagetest/storagetest.go` (new) | conformance suite |
| `internal/storage/*_test.go` (new) | local, fake-S3 and env-gated real-S3 runs; traversal |
| `internal/config/config.go`, `config.yaml` | `StorageConfig` + defaults |
| `internal/avatar/avatar.go` (+ test) (new) | `Process` |
| `internal/db/migrations/1NN_avatar_keys.sql` (new) | `avatar_key` columns |
| `internal/model/user.go`, `org.go` | `AvatarKey` |
| `internal/store/user_store.go`, `org_store.go` | scan `avatar_key`; `SwapAvatarKey`; batched key lookup |
| `internal/service/avatar_service.go` (+ test) (new) | Set/Remove for users and orgs |
| `internal/service/user_service.go`, `org_service.go`, `services.go` | delete hooks, `ErrNotOrgOwner`, wiring |
| `cmd/server/main.go` | build the backend, fail startup on bad config |
| `internal/handler/avatar_handler.go` (+ test) (new) | upload/remove/serve |
| `internal/handler/page_handler.go`, `user_handler.go` | nav avatar key; API `avatar_url` |
| `internal/router/router.go` | routes |
| `internal/view/components/avatar.templ` | `<img>` or initials |
| `internal/view/avatar.go` (new) | `AvatarURL` |
| `internal/view/pages/settings.templ`, `org_settings.templ`, `fragments/avatar_control.templ` (new) | settings control |
| identity and activity templates from spec §8 | pass URLs |
| `docker-compose.yml`, `Dockerfile`, `.gitignore` | versitygw `s3` profile, `/data/storage` |
| `docs/storage.md` (new), `configuration.md`, `deployment.md`, `api-reference.md`, `ROADMAP.md`, `CLAUDE.md` | docs |

---

## Task 1: Storage layer (ticket 01)

- [ ] Write `storagetest.Run` covering:
  - put/get/exists/delete and overwrite
  - `ErrNotFound`, and deleting a missing key
  - invalid keys
- [ ] Add `ValidKey` and `ErrNotFound`.
- [ ] `LocalBackend`:
  - `os.OpenRoot` on a `MkdirAll`'d root.
  - `Put` writes to a temp file in the target dir, then renames it.
  - Parent dirs are made through the root.
  - Run the suite on `t.TempDir()`, plus a test that `../` and absolute keys are refused.
- [ ] A fake S3 server (httptest) handles path-style PUT/GET/HEAD/DELETE. It records headers and fails a request that lacks `Authorization: AWS4-HMAC-SHA256` or carries `x-amz-checksum-*`.
- [ ] `S3Backend`:
  - `LoadDefaultConfig` with the region.
  - Use the static provider when both keys are set.
  - Set `BaseEndpoint` and `UsePathStyle`.
  - Set both checksum options to `WhenRequired`.
  - Prepend the prefix to every key.
  - Map `NoSuchKey`/`NotFound` to `ErrNotFound`.
  - Run the suite on the fake, prefix isolation included.
- [ ] Run the suite against a real server, gated on the `TEST_S3_*` env vars.
- [ ] `config.StorageConfig`: defaults for every key, and `storage.New` validation (unknown backend; `s3` without a bucket).
- [ ] `main.go` builds the backend and exits on error. `docker-compose.yml` gets a versitygw service under the `s3` profile. The Dockerfile sets `CZ_STORAGE_LOCAL_ROOT`, and `.gitignore` adds `/storage`.
- [ ] Commit: `feat(storage): local and S3-compatible object storage backends`.

## Task 2: Processing, data, service (ticket 02)

- [ ] `avatar.Process` tests first:
  - formats in: PNG, JPEG, GIF, WebP
  - rejects: SVG, HTML, a PNG renamed `.svg`, SVG declared as `image/png`
  - a 65535² header is rejected before decode
  - a 2 MB + 1 byte input is rejected
  - EXIF is stripped
  - alpha → PNG, opaque → JPEG
  - at most 460 px, square, no upscale
- [ ] Implement `Process` with only the png/jpeg/gif/webp decoders imported.
- [ ] Migration, `AvatarKey` on the models, scans.
- [ ] Store methods:
  - `UserStore.SwapAvatarKey` and `OrgStore.SwapAvatarKey`: `UPDATE … FROM (SELECT avatar_key … FOR UPDATE) RETURNING old`.
  - `UserStore.AvatarKeysByUsername(ctx, []string)`.
- [ ] `AvatarService` integration tests:
  - upload, replace, same image twice, remove
  - a failed swap cleans up the new object
  - a non-owner is refused
  - user and org delete remove the object
- [ ] Implement it, then wire the delete hooks (after commit) and `Services.Avatar`.
- [ ] Commit: `feat(avatar): process uploads and store them per user and org`.

## Task 3: Upload UI, serving, identity surfaces (ticket 03)

- [ ] Handler tests:
  - upload: 413 over the cap, including a non-htmx multipart post without `X-CSRF-Token`
  - upload: 422 for a bad image, 401 signed out, 403 for a non-owner, 404 for an unknown org
  - serve: the cache, ETag, nosniff and CSP headers; 304 on a match; 404 on a malformed or unknown key
- [ ] Implement the handlers and routes. The org routes go outside `/api/orgs`, and org changes go to the audit log.
- [ ] `components.Avatar(name, url, size, alt)` plus `view.AvatarURL`, and update every call site's signature. Commit-author sites pass `""`.
- [ ] The nav and `basePage` read `avatar_key`. `OrgEntry` gets an avatar URL.
- [ ] The settings control (user and org) follows the picked "buttons row" layout.
- [ ] Identity surfaces from spec §8.
- [ ] API `avatar_url`.
- [ ] Docs.
- [ ] Commit: `feat(ui): upload avatars from settings and show them on identity pages`.

## Task 4: Activity surfaces (ticket 04)

- [ ] For each surface in spec §8 (activity), add `avatar_key` to the author join, or a batched key lookup for username lists. Pass the URL to the component.
- [ ] Add one render test per data path that checks the `<img src>`.
- [ ] Commit: `feat(ui): show uploaded avatars on comments, pulls, discussions and lists`.

## Task 5: Verify and ship

- [ ] Full `go test ./...` with the DSN, plus a browser check on a throwaway server: upload, replace and remove on both settings pages; the avatar in the nav, on the profile and next to a comment.
- [ ] A subagent reviews the diff against the spec, and its findings get fixed.
- [ ] Tick the spec and ticket acceptance criteria and set `Status: done`. Open the PR.
