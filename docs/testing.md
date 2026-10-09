# Testing

## What CI runs

- **Test** runs `go test ./...` with no database, so every test that needs Postgres skips itself. It finishes in about a minute, which puts a compile error or a unit failure in front of you long before Integration ends, and it is the only job that runs `internal/backup` and `internal/seed` at all — don't drop it as a duplicate of Integration.
- **Integration** starts a `postgres:18-alpine` service, migrates it with `cz-admin migrate` and runs `go test` with `TEST_DATABASE_DSN` set, on every package except `internal/backup` and `internal/seed`.

Integration leaves out only the database tests of `internal/backup` and `internal/seed`; the 20 that need no database still run in the Test job. With the database on, `backup` alone took 207 s of a ~5 min job on a 2-vCPU runner (it migrates a database per test, and the seed runs bcrypt), and its round trips also want `pg_dump` and `pg_restore` of major 18 or newer, which the runner would have to install. The four real round trips therefore have no CI coverage at all: run them locally when you touch backup, restore or the schema they dump.

## Running everything locally

```bash
make test-integration
```

This starts the test database and runs `go test ./...`, backup and seed included. Without `pg_dump` and `pg_restore` (major 18 or newer) on `PATH`, the four real round-trip tests in `internal/backup/roundtrip_test.go` skip and only the stand-in scripts in `fakepg_test.go` run.

`make test-integration` does not migrate the database. On a fresh one, run `CZ_DATABASE_DSN=<the same DSN> go run ./cmd/cz-admin migrate` once first.

Run the backup and seed packages before changing `internal/backup`, `internal/seed` or the schema they dump.
