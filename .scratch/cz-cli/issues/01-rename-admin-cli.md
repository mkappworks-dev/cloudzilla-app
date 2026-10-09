# Rename cloudzilla-cli to cz-admin

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: none

Part of [spec](../spec.md). Frees the `cz` name for the remote client and separates operator tooling from developer tooling.

## What to build

Rename the operator binary `cloudzilla-cli` to `cz-admin` and move `cmd/cloudzilla/` to `cmd/cz-admin/`. Change the cobra root `Use:` from `cloudzilla` to `cz-admin`. No behaviour change.

References to update: `Makefile`, `Dockerfile` (build, `COPY`, and the comment about `backup`/`restore`), `.github/workflows/ci.yml`, `.github/workflows/build-and-publish.yml` (build and the `tar` line), `README.md`, `internal/service/admin_user_service.go`, `internal/service/health_service.go`, `docs/**`, and the `.scratch/**` specs and tickets that name the binary. Historical plans under `docs/superpowers/plans/` keep the name they were written with unless they instruct a reader to run it.

## Acceptance criteria

- [ ] `grep -rI cloudzilla-cli` finds nothing outside `docs/superpowers/plans/` history and git.
- [ ] `make build` produces `dist/cz-admin`; the Docker image has `/app/cz-admin` and no `/app/cloudzilla-cli`.
- [ ] The release archive contains `cloudzilla` and `cz-admin`.
- [ ] `make test` passes, including the command tests moved with the directory.
- [ ] Docs that tell an operator to run a command use `cz-admin`.
- [ ] The PR description tells operators to update scripts and `docker compose exec` calls that use the old name.

## Comments
