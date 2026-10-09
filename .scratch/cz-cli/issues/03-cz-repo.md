# cz repo commands

Created: 2026-10-09
Category: enhancement
Status: needs-triage
Blocked by: 02

Part of [spec](../spec.md).

## What to build

- `cz repo list [--owner X]` → `GET /api/repos/`
- `cz repo view <owner/repo>` → `GET /api/repos/:owner/:repo`
- `cz repo create <name> [--org X] [--private] [--description] [--readme] [--gitignore] [--license]` → `POST /api/repos/` or `/api/orgs/:org/repos`
- `cz repo fork <owner/repo>` → `POST /api/repos/:owner/:repo/fork`
- `cz repo clone <owner/repo>` → runs the system `git clone` with the HTTPS URL. The token is not put in the URL or on the command line: use a git credential helper entry or `GIT_ASKPASS`, and say in `docs/cli.md` which.

When a command takes `<owner/repo>` and is run inside a clone of a Cloudzilla repo, default it from the `origin` remote (shared helper for tickets 04 and 05).

A duplicate name is `422`; print the server's message.

## Acceptance criteria

- [ ] Each command works against a test server and prints a table on a terminal, JSON with `--json` or when piped.
- [ ] `repo create` needs `repo:write`; a read-only token gets the scope message from 02.
- [ ] `repo clone` never leaves the token in `.git/config`, shell history or the process list.
- [ ] The `origin`-remote default resolves `owner/repo` from HTTPS and SSH remote forms, and says so plainly when the remote isn't this host.

## Comments
