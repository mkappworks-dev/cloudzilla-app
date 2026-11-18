# End-to-end proof, docs and roadmap

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 05, 08
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-e2e-docs-roadmap` | `.worktrees/feat+phase-16-e2e-docs-roadmap` | `docs(registry): package registry docs and roadmap status (16.1–16.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

The acceptance bar is a real Docker client, not our own test client, and an operator needs one page that explains the registry, its proxy settings and its failure modes.

## Scope

- Real `docker login`, `docker push` and `docker pull` (tag and digest, a multi-platform image, a private and a public Repository, an anonymous pull) on a throwaway server; the OCI conformance suite (pull, push, content discovery, content management and referrers toggles) run locally against it; both results go in the final PR body.
- `docs/package-registry.md`: image path and linking, tokens and scopes, `docker login`, permissions, quotas, GC and orphans, generic upload, reverse proxy (`/v2/`, no body limit, no buffering), HTTPS note, `registry.enabled`, error table, known limits, and the CI secrets recipe.
- A line in CLAUDE.md's subsystem list; updates to `access-control.md`, `storage.md`, `configuration.md`, `api-reference.md`, `cli.md` and `ci.md` where earlier tickets did not; ROADMAP 16.1 and 16.2 marked done with their migration numbers; GLOSSARY checked.
- Ticket 19 in the uiux folder gets its boxes ticked and Run order rows 7 and 8 updated.

## Files (hotspots in bold)

- **`docs/package-registry.md`** (new)
- **`CLAUDE.md`**
- `docs/ROADMAP.md`
- `docs/*.md`
- `.scratch/2026-11-06-uiux-desktop-9.5/spec.md` and `issues/19-package-registry.md`

## Acceptance criteria

- [ ] A recorded transcript of a real Docker session (login, push, pull by tag and digest, a refused push, an anonymous pull) is in the PR body
- [ ] The conformance run has no failures in the enabled categories; skipped sub-tests (for example multi-tag `PUT ?tag=`) are listed with the reason, and the Repositories the suite pushes to (a name match for each of its namespaces) are created before it runs
- [ ] Docs state the single-instance assumption, that an owner rename breaks pull commands, and the accepted pull-count loss on a crash
- [ ] `docs/package-registry.md` exists, and a test greps its `registry.*`, `packages.*` and `quota.*` keys against the config defaults
- [ ] CLAUDE.md, ROADMAP, the uiux ticket 19 and its Run order are updated; `npm run fmt:check` passes for the docs (not `.scratch/`)
- [ ] Light and dark at desktop 1440 re-checked on the final integration branch for the three package pages
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Final throwaway server run on the integration branch: Docker and conformance as above, plus `make test-integration` and `golangci-lint`.

## Comments
