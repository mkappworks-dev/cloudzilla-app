# Rename cloudzilla-cli to cz-admin

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: none

Part of [spec](../spec.md). Frees the `cz` name for the remote client and separates operator tooling from developer tooling.

## What to build

Rename the operator binary `cloudzilla-cli` to `cz-admin` and move `cmd/cloudzilla/` to `cmd/cz-admin/`. Change the cobra root `Use:` from `cloudzilla` to `cz-admin`. No behaviour change.

References to update: `Makefile`, `Dockerfile` (build, `COPY`, and the comment about `backup`/`restore`), `.github/workflows/ci.yml`, `.github/workflows/build-and-publish.yml` (build and the `tar` line), `README.md`, `internal/service/admin_user_service.go`, `internal/service/health_service.go`, `docs/**`, and the `.scratch/**` specs and tickets that name the binary.

## Acceptance criteria

- [ ] `grep -rI cloudzilla-cli` finds nothing outside git history.
- [x] `make build` produces `dist/cz-admin`; the Docker image has `/app/cz-admin` and no `/app/cloudzilla-cli`.
- [x] The release archive contains `cloudzilla` and `cz-admin`.
- [x] `make test` passes, including the command tests moved with the directory.
- [x] Docs that tell an operator to run a command use `cz-admin`.
- [ ] The PR description tells operators to update scripts and `docker compose exec` calls that use the old name.

## Comments

Claude, 2026-10-10: ticked the build, release archive and test criteria after checking them against `main`. Left unticked: the `grep -rI cloudzilla-cli` criterion, which still matches this ticket, the cz-cli spec and `docs/configuration.md` (a deliberate note about old audit rows), and the PR-description criterion, which can't be checked from the repo.
