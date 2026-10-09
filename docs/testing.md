# Testing

## What CI runs

- **Test** runs `go test ./...` with no database: every test that needs Postgres skips.
- **Integration** starts a `postgres:18-alpine` service, migrates it with `cz-admin migrate` and runs `go test` with `TEST_DATABASE_DSN` set, on every package except `internal/backup` and `internal/seed`.

`internal/backup` and `internal/seed` stay out of CI: with the database on, `backup` alone took 207 s of the ~5 min job on a 2-vCPU runner (it migrates a database per test, and the seed runs bcrypt), and the backup round trips also need `pg_dump` and `pg_restore` of major 18 or newer. Their tests are unchanged and still skip themselves when the database or tools are missing.

## Running everything locally

```bash
make test-integration
```

This starts the test database and runs `go test ./...`, backup and seed included. Without `pg_dump` and `pg_restore` (major 18 or newer) on `PATH`, the four real round-trip tests in `internal/backup/roundtrip_test.go` skip and only the stand-in scripts in `fakepg_test.go` run.

`make test-integration` does not migrate the database. On a fresh one, run `CZ_DATABASE_DSN=<the same DSN> go run ./cmd/cz-admin migrate` once first.

Run the backup and seed packages before changing `internal/backup`, `internal/seed` or the schema they dump.
