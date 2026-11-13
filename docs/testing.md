# Testing

## What CI runs

- **Test** runs `go test ./...` with no database, so every test that needs Postgres skips itself. It finishes in about a
  minute, which puts a compile error or a unit failure in front of you long before Integration ends, and it is the only
  job that runs `internal/backup` and `internal/seed` at all — don't drop it as a duplicate of Integration.
- **Integration** starts a `postgres:18-alpine` service, migrates it with `cz-admin migrate` and runs `go test` with
  `TEST_DATABASE_DSN` set, on every package except `internal/backup` and `internal/seed`.

Integration leaves out only the database tests of `internal/backup` and `internal/seed`; the 20 that need no database
still run in the Test job. With the database on, `backup` alone took 207 s of a ~5 min job on a 2-vCPU runner (it
migrates a database per test, and the seed runs bcrypt), and its round trips also want `pg_dump` and `pg_restore` of
major 18 or newer, which the runner would have to install. The real round trips (the four in
`internal/backup/roundtrip_test.go` and `TestBackupThenRestore_WithTheRealPostgresTools` in `cmd/cz-admin`) therefore
have no CI coverage at all: run them locally when you touch backup, restore or the schema they dump.

The `cz-admin` `backup`, `restore` and `seed` command tests do run in Integration: they use stand-in `pg_dump` and
`pg_restore` scripts, a scratch database and a three-user seed, and add about 8 s.

Neither job passes a build tag, so **nothing under `//go:build dev` is ever compiled in CI** — the dev loop's
disk-served assets, `/_dev/` routes and layout hook included. A change that breaks only the dev build passes both jobs.
Run `go build -tags dev ./...` and `go test -tags dev ./internal/assets/` locally when you touch any of it; see
[dev-loop](./dev-loop.md).

## Secret scanning tests

The push-protection tests push real packs, built by `internal/testutil/pushclient.go`, through both transports and assert
the exact ref-status text: `internal/handler/git_push_protection_test.go` (HTTP) and
`internal/ssh/push_protection_test.go` (SSH). They need Postgres for the alert tables, so they skip in the Test job. The
scanner's tests (`internal/secretscan`) need nothing. Before changing the push path, also run native `git push` over
HTTP with a token and over SSH against a throwaway server and read the refusal line: the go-git client in the tests
cannot show what real git prints. See [secret-scanning](./secret-scanning.md#what-an-end-to-end-run-showed).

## Running everything locally

```bash
make test-integration
```

This starts the test database and runs `go test ./...`, backup and seed included. Without `pg_dump` and `pg_restore`
(major 18 or newer) on `PATH`, the four real round-trip tests in `internal/backup/roundtrip_test.go` skip and only the
stand-in scripts in `fakepg_test.go` run.

`make test-integration` does not migrate the database. On a fresh one, run
`CZ_DATABASE_DSN=<the same DSN> go run ./cmd/cz-admin migrate` once first.

Run the backup and seed packages before changing `internal/backup`, `internal/seed` or the schema they dump.

## Local desktop accessibility checks

The local Playwright harness uses pinned Chromium and axe versions. It scans the desktop route list in light and dark at
1440px, and exercises the repository selectors, milestone tabs and wiki reorder by keyboard. These checks are not part
of CI. Serious and critical findings fail unless their exact route, rule and target are documented in
`tests/a11y/allowlist.js`.

To run the browser checks locally, start a disposable server using the spec's unique ports, cookie name and storage
paths. Migrate it and seed with `cz-admin seed --users 12 --orgs 2 --repos 10`, then:

```sh
cd tests/a11y
npm ci
npx playwright install chromium
CZ_A11Y_BASE_URL=http://127.0.0.1:18631 npm test
```

Use a throwaway instance: setup creates wiki pages and a gist. When fixing an allow-listed finding, remove its entry;
new exceptions should identify the affected node and explain why the issue remains.

For the spec's visual harness, run `npm run screenshots` from `tests/a11y` against the same disposable server. It starts
the standing-mock server, builds its Tailwind CSS, and captures every listed real route and standing mock in both themes
at 1440px. Files go to `.cache/screenshots/<theme>/`; set `CZ_A11Y_SCREENSHOTS` to choose an evidence directory. The
mock server uses port 18633 and exits when capture finishes.
