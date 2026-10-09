# cz: the remote client

`cz` is a command-line client for a Cloudzilla host. It is a plain HTTP client over `/api/**`: it never opens the database or reads `config.yaml`, so it runs on any machine that can reach the server. `cz-admin` is the separate operator tool that runs on the server host (see [configuration](./configuration.md)).

## Install

Each release attaches a standalone `cz_<tag>_<os>_<arch>.tar.gz` (with a `.sha256`) for every platform the server release covers, next to the `cloudzilla_<tag>_<os>_<arch>.tar.gz` server archive. The `cz` archive holds one binary; unpack it onto your `PATH`.

From a checkout, `make build` writes `dist/cz`, or `make build-cz` builds only it. `cz` is cgo-free and needs no CSS or templ step.

The server archive and Docker image don't include `cz`.

## Logging in

```bash
cz auth login --host https://git.example.com        # prompts for a token
echo "$TOKEN" | cz auth login --host https://git.example.com --with-token
cz auth status
cz auth logout
```

Create a personal access token under your account settings, with the scopes from [the table below](#scopes-per-command). `login` verifies the token with one call to `GET /api/user` before saving it, and prints the host, user and where the token went.

| Flag                 | Effect                                                             |
| -------------------- | ------------------------------------------------------------------ |
| `--host URL`         | The Cloudzilla URL; `CZ_HOST` is used when omitted                 |
| `--with-token`       | Read the token from stdin (required when stdin isn't a terminal)   |
| `--insecure-storage` | Store the token in a `0600` file instead of the OS keychain        |

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

The `repo`, `issue` and `pr` commands arrive with tickets 03 to 05 of the `cz` spec; the rows above are their planned scopes, not yet in `cz --help`. Remove this paragraph once they land.

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
