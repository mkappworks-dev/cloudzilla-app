# End-to-end proof, docs and roadmap

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
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

- [ ] A transcript of the real Docker session is in the PR body
- [ ] The conformance run has no failures in the enabled categories; skipped sub-tests (`PUT ?tag=` multi-tag at least) are listed with the reason
- [ ] A test reads `docs/package-registry.md` and fails if a `registry.*`, `packages.*` or `quota.*.package_bytes` key it names has no config default, or a default has no mention
- [ ] CLAUDE.md, ROADMAP, the uiux ticket 19 and its Run order are updated; `npm run fmt:check` passes (Prettier is never run on `.scratch/`)
- [ ] The three package pages and the owner tab re-checked at desktop 1440 in **light and dark** on the integration branch, with the standing mocks matching
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Final throwaway server run on the integration branch: Docker and conformance as above, plus `make test-integration` and `golangci-lint run`.

## Comments
