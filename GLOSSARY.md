# Cloudzilla glossary

Canonical vocabulary. Terms here are the only names used in code, docs and the UI. This file is a glossary: no
decisions, no implementation.

See [docs/](docs/) for how each area works.

## Accounts

**Instance** — one running Cloudzilla server and its database. Instance roles are `superadmin` and `user`.

**Superadmin** — an instance role: settings, SSO, accounts and invitations. There is always at least one active
superadmin.

**Admin mode** — a superadmin's time-boxed, audited access to any Repository or Organization, held per browser. Outside
it a Superadmin is a normal User there.

**User** — an individual account. Its username is a namespace that can own Repositories.

**Organization** — a shared namespace that owns Repositories and has Members. Users and Organizations share one
case-insensitive name space; `/{owner}` resolves a User first, then an Organization.

**Org role** — `owner` or `member`. A `member` has no implicit access to private Organization Repositories.

## Repositories

**Repository** — a Git repository with its issues, pull requests, wiki and settings. Owned by exactly one User or
Organization.

**Owner** — the User or Organization a Repository belongs to; `/{owner}/{repo}` names it.

**Collaborator** — a User with an explicit Repository role: `reader`, `writer` or `admin`. Only the Owner can grant or
revoke `admin`.

**Pull request** — a request to merge a head branch into a base branch, merged by fast-forward, merge commit or squash.

**Wiki** — a Repository's Markdown pages, stored in a separate bare Git repository named `<repo>.wiki.git`.

**Import** — a one-off background copy of a Git repository from another host into a new Repository.

**Pull mirror** — a read-only Repository that keeps its branches and tags equal to an upstream's on a schedule. It
starts as an Import.

## Credentials

**Personal access token** — a user-created, scoped credential for the API and for Git over HTTP.

**OAuth app token** — a scoped credential issued to a third-party application the User approved.

**Deploy key** — an SSH public key attached to one Repository rather than a User.

**Device login** — the `cz auth login` flow: a code typed at `/login/device`, approved in the browser, exchanged for a
token.

## Tools

**`cz`** — the remote command-line client for developers. It talks to the JSON API only.

**`cz-admin`** — the operator command-line tool. It runs on the server host and reads `config.yaml` and the database.
