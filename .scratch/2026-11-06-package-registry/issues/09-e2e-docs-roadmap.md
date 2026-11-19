# End-to-end proof, docs and roadmap

Created: 2026-11-18
Category: enhancement
Status: done
Blocked by: 08
Parallel with: none

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-e2e-docs-roadmap` | `.worktrees/feat+phase-16-e2e-docs-roadmap` | `docs(registry): package registry docs, end-to-end proof and roadmap (16.1–16.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

The acceptance bar is a real Docker client, not our own test client, and an operator needs one page that explains the registry, its proxy settings and its failure modes.

## Scope

- Real `docker login`, `docker push` and `docker pull` (tag and digest, a multi-platform image, a private and a public Repository, an anonymous pull, a refused push) on a throwaway server; the OCI conformance suite (pull, push, content discovery, content management, referrers) run locally against it, with the Repositories it pushes to created first (a name match for each of its namespaces). Both results go in the final PR body.
- `docs/package-registry.md` in its final shape: path and linking, tokens and scopes, `docker login`, the status-code table, permissions and transfers, quotas, GC and orphans, generic upload, reverse proxy (`/v2/`, no body limit, no buffering), HTTPS, `registry.enabled`, the single-instance assumption, an owner rename breaking pulls, and the CI secrets recipe.
- A line in CLAUDE.md's subsystem list; cross-links from `access-control.md`, `storage.md`, `configuration.md`, `api-reference.md`, `cli.md`, `ci.md` and `organizations.md`; ROADMAP 16.1 and 16.2 marked done with their migration numbers; GLOSSARY checked.
- Ticket 19 in the uiux folder ticked and its Run order rows 7 and 8 updated.

## Files (hotspots in bold)

- **`docs/package-registry.md`**
- **`CLAUDE.md`**
- `docs/ROADMAP.md`, `docs/*.md`
- `.scratch/2026-11-06-uiux-desktop-9.5/spec.md`, `issues/19-package-registry.md`

## Acceptance criteria

- [x] A transcript of the real Docker session is in the PR body
- [x] The conformance run has no failures in the enabled categories; skipped sub-tests (`PUT ?tag=` multi-tag at least) are listed with the reason
- [x] A test reads `docs/package-registry.md` and fails if a `registry.*`, `packages.*` or `quota.*.package_bytes` key it names has no config default, or a default has no mention
- [x] CLAUDE.md, ROADMAP, the uiux ticket 19 and its Run order are updated; `npm run fmt:check` passes (Prettier is never run on `.scratch/`)
- [x] The three package pages and the owner tab re-checked at desktop 1440 in **light and dark** on the integration branch, with the standing mocks matching
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Final throwaway server run on the integration branch: Docker and conformance as above, plus `make test-integration` and `golangci-lint run`.

## Comments

### 2026-11-19

- Real Docker session (Docker 29.9.3, containerd store) on a containerised server and scratch DB `cz_e2e`: login with a `cz`-minted
  token, refusal of a `repo:write` token, a wrong token and a password; push (single-platform and `buildx` linux/amd64+arm64), push
  to a private Repository, pull by tag and digest, a second tag sharing layers, cross-package mount, anonymous pull (public ok, private
  refused), reader pull and refused push, stranger refused, and an image that cannot be linked refused with the two-routes message.
  Transcript `/private/tmp/czpkg-e2e/e2e-docker-transcript.txt` (script `e2e-docker.sh`). Docker's credential store keeps one login per
  registry host (macOS keychain), so the stranger and anonymous runs use `127.0.0.1`.
- Conformance (distribution-spec `v1.1.1-57-g9727462`, `OCI_VERSION=1.1`, `OCI_TLS=disabled`, repos `conformance/repo1`, `repo2`):
  725 tests, 0 failures, 1 skipped; reports in `/private/tmp/czpkg-e2e/{report.html,junit.xml,results.yaml,conformance.log}`. Skipped or
  disabled with reasons: anonymous mount (the server answers `202` and the suite falls back, as the spec allows), `PUT ?tag=` multi-tag
  and tag-param tests (out of scope, decision list), sha512 blobs and digests (`OCI_DATA_SHA512=false`, sha256 only), sparse manifests
  (off by default). The run sets `registry.max_sessions_per_user` to 1000 because the suite starts uploads it never finishes.
- First conformance run failed 738 of 1029; the causes, all in the registry: the 10-session cap (config, above); no `delete` action in a
  token for a name with no linked package, so blob `DELETE` before the first manifest was `401` (fixed: a writer's new-name grant
  includes `delete`); referrers for a name nobody pushed was `404` instead of an empty index (fixed); referrers descriptors lacked the
  manifest's `annotations` (fixed). Each fix has a router test written first.
- GC live (`registry.gc_grace=1m`): pushed two images sharing a base layer, deleted one by digest; restart 1 after the grace dropped its
  links and stamped its two blobs; restart 2 deleted both objects (`registry/` 5 -> 3 objects, `registry_blobs` 5 -> 3); the shared layer
  stayed and the other image still pulls. Transcript `gc-live-transcript.txt`.
- Also: `GET /v2/token` answered `500` twice during a mixed-identity Docker run and I could not reproduce it (30 concurrent curl requests,
  isolated Docker configs); the handler now logs the error, so a recurrence shows up. `make test-integration` starts its own database;
  I ran `go test ./...` with `TEST_DATABASE_DSN` on `cz_t09` (same tests) and `golangci-lint run`, both clean.
- Pages re-checked at 1440 in light and dark (list, container detail, generic detail, owner tab): `/private/tmp/czpkg-e2e/shots/`. No page
  or mock changed in this branch.
- Docs: `docs/package-registry.md` (final shape), `internal/config/package_registry_doc_test.go` (keys named vs defaults), cross-links,
  CLAUDE.md line, ROADMAP 16.1/16.2 (migration 142), uiux ticket 19 and Run order. GLOSSARY already has the package terms.

- 2026-11-19, review fixes (`fix/phase-16-review`): a first push to a name matching a private Repository the pusher cannot read now gets the
  same refusal as a name that matches nothing; the JSON container GET/DELETE/relink look the package up by linked Repository, so they keep
  working after a transfer; an owner segment that can never be a valid image path is named in the `NAME_INVALID` message; `MountBlob`
  checks the target owner's quota before linking. Standards: ticket narration and "not available yet" wording gone from `Registry`,
  `ownerPackagesData` moved into `user_page_handler.go`, `writeJSONAs` for the OCI index type, one `finishPackageChange`, `errDigestInvalid()`
  and `RegCode*` constants, `packageForm`/`ownerPackageForm` return structs. Spec: Decisions 9 and 22 and the Middleware bullet corrected.
  Not changed: a linked name whose Repository is soft-deleted already answered with empty access (`GetByID` filters `deleted_at`); a router
  test now pins it.
