# cz: a remote command-line client

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

## Problem

Cloudzilla has no developer-facing CLI. The only binary, `cloudzilla-cli` (`cmd/cloudzilla`), is an operator tool: it reads `config.yaml` and opens the database, so it only works on the server host and can't create a repo, open a PR or comment on an issue.

The pieces for a `gh`-style client exist. `/api/**` covers repos, issues, pulls, reviews and more (`docs/api-reference.md`), and personal access tokens (`czp_…`) with scopes `repo:read`, `repo:write`, `issues:write` and `pulls:write` authenticate it. Nothing drives them from a terminal.

Example: a developer who wants to open a PR from a branch they just pushed has to switch to the browser, find the repo, pick the branches and fill the form.

## Proposed design

A new binary, `cz`, built from `cmd/cz`. It is a plain HTTP client over `/api/**`: it never imports `internal/store` or `internal/service` and never touches the database or `config.yaml`.

The existing `cloudzilla-cli` is renamed `cz-admin` (`cmd/cz-admin`) so the two stay visibly apart: `cz` for developers, `cz-admin` for the operator.

### Command set for the first release

- `cz auth login | status | logout`
- `cz repo list | view | create | clone | fork`
- `cz issue list | view | create | comment | close`
- `cz pr list | view | create | merge | close | review`
- `cz api <method> <path>`: an authenticated raw request, for anything not wrapped

Later, not in this spec: `release`, `label`, `milestone`, `notification`, `search`, `gist`, `webhook`, `pr checkout`, `pr diff` (the last two need endpoints the API doesn't list).

### Authentication, in two stages

**Stage 1: personal access token** (tickets 02–06).
- `cz auth login --host <url>` prompts for a PAT, or reads one from stdin with `--with-token`. It verifies the token with one authenticated call before saving it.
- `CZ_TOKEN` and `CZ_HOST` override the stored values, for CI.
- The token goes to the OS keychain; when none is available it falls back to a `0600` file under the user config dir. `cz auth status` says which one is in use, and `--insecure-storage` forces the file, as `gh` does.
- A token can't do everything. `pulls:write` doesn't merge (merging needs `repo:write`); account, admin and import routes are closed to tokens; `repo:admin` tokens need every request signed with an SSH key. `cz` doesn't sign requests. When the server answers `403 insufficient_scope`, `cz` prints the missing scope from the `WWW-Authenticate` header.

**Stage 2: device-code flow** (ticket 07), modelled on GitHub's OAuth device flow.
- `cz auth login` asks the server for a device code and prints a short user code and a URL. The user approves in the browser, where the existing login and 2FA apply, and `cz` polls until it gets a token. No client secret; the token has the same format and scopes as a PAT.
- Needs a migration for pending grants (15-minute expiry), an approval page that re-checks the password and 2FA, rate limits on code entry and polling, and `authorization_pending` / `slow_down` / `expired_token` / `access_denied` responses.
- Its own ticket, `needs-triage`, because it adds server surface and security review.

### Layout

- `cmd/cz/`: cobra root and one file per command group, as `cmd/cloudzilla/` does.
- A small API client package (`internal/cli/` or `cmd/cz/internal/`; decide in ticket 02) holding the host/token resolution, the keychain-or-file store, request building and error mapping. Command files call it and don't build requests.
- JSON output by default for a non-terminal stdout; a `--json` flag for scripts.

## Acceptance criteria

- [ ] `cloudzilla-cli` is `cz-admin` everywhere: binary, `cmd/` directory, Makefile, Dockerfile, CI, release archive, README and docs. Nothing refers to the old name.
- [ ] `cz auth login` stores a verified token; `status` shows host, user and storage; `logout` removes it.
- [ ] The core commands above work against a real server with a PAT carrying the right scopes.
- [ ] A missing scope, a bad token and an unreachable host each give a one-line message and a non-zero exit.
- [ ] `cz` builds without importing the store or service layers (checked in CI or by a test on its imports).
- [ ] `cz` ships in the release archive and the Docker image's `dist/`, and is documented in `docs/cli.md`, linked from `CLAUDE.md`'s subsystem list.

## Relevant files

- `cmd/cloudzilla/` (renamed in 01), `Makefile`, `Dockerfile`, `.github/workflows/ci.yml`, `.github/workflows/build-and-publish.yml`, `README.md`
- `internal/middleware/scope.go` (the path allow-list that decides what a token can reach)
- `docs/api-reference.md`, `docs/access-control.md` (token scopes, signed requests)
- `internal/handler/` API handlers for repos, issues and pulls, to check each returns clean JSON to a non-HTMX caller

## Decisions

Agreed with the maintainer on 2026-10-09.

1. **New binary, not extra subcommands.** The client must not carry code that opens the database, and nothing existing breaks. The cost is two release artifacts.
2. **Command name `cz`; admin binary renamed `cz-admin`.**
3. **PAT first, device flow second**, as `gh auth login` offers a browser device flow plus `--with-token`/`GH_TOKEN`. The device flow is the better experience but is server work, so it doesn't gate the client.
4. **Core command set above**; the rest follows once it is used.
5. **Client package is `internal/cli/`; keychain library is `github.com/zalando/go-keyring`** (decided 2026-10-09, ticket 02). A package under `internal/` lets tickets 03-05 and `cmd/cz` share it without a nested `cmd/cz/internal/`, and a test on `go list -deps` keeps it off the store and service layers. go-keyring is pure Go on macOS, Linux and Windows, so `cz` stays cgo-free and cross-compiles.

## Plan

Tickets in `issues/`, in order:

1. `01-rename-admin-cli` — mechanical rename; land first and alone to keep conflicts with open branches small.
2. `02a-whoami-endpoint` — server: `GET /api/user` so `cz auth login` can verify a token and learn the username. Found while starting 02.
3. `02-cz-skeleton-auth` — `cmd/cz`, API client package, token store, `auth` commands, `api`.
4. `03-cz-repo` — repo commands.
5. `04-cz-issue` — issue commands.
6. `05-cz-pr` — PR commands.
7. `06-cz-build-release-docs` — Makefile, Dockerfile, CI, release archive, `docs/cli.md`.
8. `07-device-code-login` — stage 2; `needs-triage`.
