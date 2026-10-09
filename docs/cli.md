# cz: the remote client

`cz` is a command-line client for a Cloudzilla host. It is a plain HTTP client over `/api/**`: it never opens the database or reads `config.yaml`, so it runs on any machine that can reach the server. `cz-admin` is the separate operator tool that runs on the server host (see [configuration](./configuration.md)).

## Install

Each release attaches a standalone `cz_<tag>_<os>_<arch>.tar.gz` (with a `.sha256`) for every platform the server release covers, next to the `cloudzilla_<tag>_<os>_<arch>.tar.gz` server archive. The `cz` archive holds one binary; unpack it onto your `PATH`.

From a checkout, `make build` writes `dist/cz`, or `make build-cz` builds only it. `cz` is cgo-free and needs no CSS or templ step.

The server archive and Docker image don't include `cz`.

## Logging in

```bash
cz auth login --host https://git.example.com                   # approve in the browser
cz auth login --host https://git.example.com --scope repo:read # ask for less
echo "$TOKEN" | cz auth login --host https://git.example.com --with-token
cz auth status
cz auth logout
```

`login` runs the device flow (RFC 8628) by default. It prints a one-time code and the URL `<host>/login/device`, you sign in there in a browser (2FA works) and type the code, and `cz` stores the personal access token the server then issues. Like the `--with-token` path, it verifies the token with one call to `GET /api/user` before saving it, and prints the host, user and where the token went.

What `cz` shows and does:

- The code (`XXXX-XXXX`) and the URL go to stderr, always. The code is never part of a URL: you read it from your terminal and type it, so a link someone sends you can't skip that check.
- The code is copied to the clipboard where a tool exists (`pbcopy`, `wl-copy`, `xclip`, `xsel`, `clip.exe`), silently otherwise.
- The plain URL is opened in your browser only when stdin and stdout are both terminals and `--no-browser` isn't set.
- Nothing is read from stdin, so it works headless and over SSH: open the URL on any machine, sign in, and type the code. `cz` polls until you approve, deny or the code expires (15 minutes), or you press Ctrl-C. Nothing is stored unless the login succeeds.

The token is a normal personal access token named `cz (<hostname>) · <date>`, with no expiry. It appears under Settings, Access tokens, where you can revoke it. It asks for `repo:write` by default (the scope that admits merging, creating and forking repositories and pushing); the approval page lets you untick scopes but never add any. `repo:admin` can't be requested this way: create that token under Settings and use `--with-token`.

An older server without the endpoint answers `404`, and `cz` points you to `--with-token`. In that case, create a personal access token under your account settings with the scopes from [the table below](#scopes-per-command) and pipe it in.

| Flag                 | Effect                                                                                                      |
| -------------------- | ----------------------------------------------------------------------------------------------------------- |
| `--host URL`         | The Cloudzilla URL; `CZ_HOST` is used when omitted                                                          |
| `--scope S`          | Scope to request: `repo:read`, `repo:write`, `issues:write`, `pulls:write`. Repeatable or space-separated; default `repo:write` |
| `--no-browser`       | Print the URL and code without opening a browser                                                            |
| `--with-token`       | Skip the browser: read a personal access token from stdin                                                   |
| `--insecure-storage` | Store the token in a `0600` file instead of the OS keychain                                                 |

`cz auth status` fails when nobody is logged in.

### Environment variables

| Variable   | Effect                                                                 |
| ---------- | ---------------------------------------------------------------------- |
| `CZ_TOKEN` | Token to use instead of the stored one, for CI                         |
| `CZ_HOST`  | Host to use instead of the stored one                                  |

With both set, nothing is read from storage. With `CZ_HOST` alone, `cz` refuses to send the stored token to a different host than the one it was saved for and asks you to set `CZ_TOKEN` too. `CZ_TOKEN` alone needs a stored login to supply the host.

### Where the token lives

The token goes to the OS keychain. When none is available, or with `--insecure-storage`, it goes to `credentials.json` in a `cz` directory under `os.UserConfigDir()` (`~/.config/cz/credentials.json` on Linux, `~/Library/Application Support/cz/credentials.json` on macOS), mode `0600`. `cz auth status` names the one in use.

## Output

On a terminal, commands print human-readable text. `--json`, or a stdout that isn't a terminal, prints JSON instead. Errors are one line on stderr prefixed `cz:`, with a non-zero exit: an unreachable host, a bad token (`401`, run `cz auth login`) and a missing scope each get their own message. A `429` reports the `Retry-After` wait.

## Scopes per command

A token acts as its user but only on the routes its scopes admit ([Token Scopes](./access-control.md#token-scopes)). When the server answers `403 insufficient_scope`, `cz` prints the scope from the `WWW-Authenticate` header.

| Command                                                              | Scope needed                                                       |
| -------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `cz auth login`, `status`                                            | any scope (calls `GET /api/user`)                                  |
| `cz auth logout`                                                     | none; removes the local login only                                 |
| `cz repo list`, `view`                                               | `repo:read`                                                        |
| `cz repo create`, `fork`                                             | `repo:write`                                                       |
| `cz repo clone`                                                      | `repo:read` (git fetch)                                            |
| `cz issue list`, `view`                                              | `repo:read`                                                        |
| `cz issue create`, `comment`, `close`                                | `issues:write` (or `repo:write`)                                   |
| `cz pr list`, `view`                                                 | `repo:read`                                                        |
| `cz pr create`, `close`, `review`                                    | `pulls:write` (or `repo:write`)                                    |
| `cz pr merge`                                                        | `repo:write`. `pulls:write` can't merge or enable auto-merge       |
| `cz api <METHOD> <path>`                                             | whatever the request needs                                         |

## Repositories, issues and pull requests

Commands that take a repository read it from `-R owner/repo`, or from the `origin` remote of the current checkout. A remote on another host is refused with a message saying so. HTTPS, `ssh://` and `git@host:owner/repo` remotes work.

Body text comes from `--body`, `--body-file` (`-` reads stdin), or `$VISUAL`/`$EDITOR` when run on a terminal with neither.

```bash
cz repo list [--owner alice]
cz repo view alice/site
cz repo create site [--org acme] [--private] [-d text] [--readme] [--gitignore Go] [--license mit]
cz repo fork alice/site
cz repo clone alice/site [directory] [-- git-clone-flags]

cz issue list [-R alice/site] [--state open|closed]
cz issue view 12
cz issue create -t "Broken link" [-b text] [-l bug] [-a bob]
cz issue comment 12 -b text
cz issue close 12

cz pr list [--state open|closed|merged|all]
cz pr view 7
cz pr create -t "Fix links" [-H branch] [-B main] [-b text] [--draft]
cz pr merge 7 [--ff | --merge | --squash]
cz pr close 7
cz pr review 7 --approve | --request-changes -b text | --comment -b text
```

- `repo list --owner` filters on the client, because the API has no owner filter.
- `issue list --state` and `pr list --state` filter on the client. The server returns every state, and at most 500 issues, with no paging.
- `issue create` looks up each `--label` by name before creating anything, so an unknown label fails with no issue created. If a label or assignee step fails after the issue exists, the error gives its number and URL.
- `pr create` defaults `--head` to the current branch and `--base` to the repository's default branch. It checks that the head branch is on the remote and refuses if it isn't; the server would accept any head name.
- `pr merge` needs `repo:write`. A `pulls:write` token is told so. Conflicts, requested changes, branch protection and required checks come back as the server's reason with a non-zero exit.
- `pr close` refuses a pull request that is no longer open.
- `pr review --request-changes` and `--comment` need a body.

### How `repo clone` and `pr create` use the token

Both run the system `git`. The token is handed to a URL-scoped credential helper through the `CZ_GIT_TOKEN` environment variable of the git child process, and `GIT_TERMINAL_PROMPT=0` is set. It never appears in the URL, the arguments, `.git/config` or shell history. Other processes of the same user can still read a running process's environment (for example with `ps eww` on some systems), so don't run `cz` on a machine where you don't trust those users.

## cz api

```bash
cz api GET /api/repos/alice/site/issues -f state=open
cz api POST /api/repos/alice/site/issues -f title="Broken link" -f priority=2
cz api POST /api/repos/alice/site/issues --input issue.json
```

- `-f key=value` (repeatable) goes in the query string for `GET` and `HEAD`, and otherwise in a JSON body. `true`, `false` and integers are sent as that JSON type.
- `--form` sends the fields as `application/x-www-form-urlencoded` instead of JSON.
- `--input FILE` sends a file as the JSON body (`-` reads stdin). It can't be combined with `-f`.
- The path must start with `/` and is sent to the logged-in host.

## Safety behaviour

- Redirects are never followed. A bad token on a page route redirects to the HTML login, and the `Authorization` header must not travel further; `cz` reports the redirect instead.
- The token is sent only to the host it was stored for (see the host-mismatch refusal above).

## What cz can't do

- Admin routes (`/api/admin/*`), account routes (`/api/user/*` such as SSH keys, tokens and 2FA), notifications, gists and OAuth apps are closed to tokens whatever their scopes.
- `repo:admin` tokens (collaborators, deploy keys, webhooks, transfer, delete) need every request signed with an SSH key. `cz` doesn't sign requests.
- Repository imports aren't available over token auth.
- Instance operation (migrations, backup, password resets) belongs to `cz-admin`, which needs the server's config and database.
