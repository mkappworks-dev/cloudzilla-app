# Build, ship and document cz

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02

Part of [spec](../spec.md). Can land with 02 or after the command tickets.

## What to build

- `Makefile`: a `build` step producing `dist/cz`.
- `.github/workflows/ci.yml`: build `cmd/cz`.
- `.github/workflows/build-and-publish.yml`: build `cz` and ship it as its own archive (per OS/arch) so developers don't download the server. The server archive keeps `cloudzilla` and `cz-admin`.
- `Dockerfile`: leave `cz` out of the server image unless there's a reason; say which in the PR.
- `docs/cli.md`: install, `cz auth login`, token scopes per command (including `pulls:write` vs `repo:write` for merge), environment variables, storage, and what `cz` can't do (admin routes, `repo:admin` tokens, imports). Link it from `CLAUDE.md`'s subsystem docs and `README.md`.
- `docs/configuration.md` / `docs/deployment.md`: note that the operator binary is `cz-admin`, and `cz` is separate.

## Acceptance criteria

- [ ] `make build` produces `dist/cz`; CI builds it.
- [ ] A release produces a standalone `cz` archive for each platform the server release covers.
- [x] `docs/cli.md` exists and is linked from `CLAUDE.md` and `README.md`.
- [ ] The docs state every command's required scope.

## Comments
