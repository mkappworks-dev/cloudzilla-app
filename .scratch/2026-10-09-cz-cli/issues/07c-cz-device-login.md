# cz auth login: device flow

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 07a

Part of [07](./07-device-code-login.md); read its Design section first. Needs 07b only for a manual end-to-end run; tests use a fake server.

## What to build

- Device flow as the default of `cz auth login` (`cmd/cz/auth.go`, request code and polling in `internal/cli/`; `internal/cli` must stay off the store and service layers).
- Request `repo:write` by default; `--scope` (repeatable or space-separated) narrows; send the hostname as `device_name`.
- Print the verification URL and user code to stderr always; copy the code to the clipboard where one is available (failing silently); open the plain URL, with no code in it, only on a TTY and without `--no-browser`. No stdin is needed, so it works headless and over SSH.
- Poll at `interval`; add 5 s on `slow_down`; stop at `expires_in`; exit on `access_denied` and `expired_token` with a one-line message; cancel cleanly on Ctrl-C.
- Verify the token with `GET /api/user` and store it exactly as the PAT path does (keychain or `0600` file, `--insecure-storage`).
- `--with-token` and `CZ_TOKEN` keep working. A `404` from the code endpoint prints a pointer to `--with-token`.
- `docs/cli.md`: the new default, flags, headless use, and that the token is revocable under Settings → Tokens.

## Acceptance criteria

- [x] Against a fake server, login completes through `authorization_pending`, then success, and stores the token.
- [x] `slow_down` lengthens the poll interval by 5 s; the interval never drops below what the server set.
- [x] `access_denied`, `expired_token` and Ctrl-C each exit non-zero with a one-line message and store nothing.
- [x] With no TTY on stdin or stdout the flow still runs and prints the URL and code; `--no-browser` suppresses opening.
- [x] The URL cz prints or opens never contains the code.
- [x] A server without the endpoint (`404`) gets the `--with-token` hint.
- [x] `--scope repo:read` is sent as requested; `repo:admin` is refused client-side with a clear message.
- [x] The import test still shows `cz` off the store and service layers.
- [x] `docs/cli.md` is updated.

## Comments

2026-10-09: built against a fake server (`cmd/cz/auth_device_test.go`, `internal/cli/device_test.go`); no end-to-end run against a real server yet, which needs 07b. The old interactive token prompt is gone, since the device flow replaced it as the default; `--with-token` is the only way to paste a token.
