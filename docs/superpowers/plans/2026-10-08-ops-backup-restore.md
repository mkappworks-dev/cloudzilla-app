# Backup and restore — Implementation Plan

> Execute one task at a time, test first, then a review pass over the whole diff.

**Goal:** `cloudzilla-cli backup` writes one tar holding the database, every repository and the SSH host key; `cloudzilla-cli restore` rebuilds an empty instance from it.

**Architecture:** `internal/backup` owns the archive format (`Manifest`, entry-name validation), the capture order (`Create`: dump, then per repo refs before objects) and the restore sequence (`Restore`: validate whole archive, check preconditions, `pg_restore`, extract, host key, migrate, reconcile). `cmd/cloudzilla/{backup,restore}.go` only parse flags and wire config. `pg_dump` and `pg_restore` run as child processes; the connection goes in `PG*` environment variables so the password never reaches the process list.

**Spec:** [`.scratch/ops-health-metrics-backup/issues/03-backup-restore.md`](../../../.scratch/ops-health-metrics-backup/issues/03-backup-restore.md)

## Global Constraints

- The archive is built in a temp file beside the output, then renamed: a failure leaves no partial output. The manifest comes first in the tar but needs final counts, so its slot is a fixed 4 KiB, patched at the end.
- `Restore` reads the whole tar once, writing nothing, before it touches the database or the disk. A bad entry name or type therefore fails before anything is extracted.
- Stores → services → handlers does not apply: the CLI calls `internal/backup` and `store.Repo.ListAll` (for the reconciliation report).
- Stage files by explicit path. Never stage `.claude/`. Comments state only a *why*.

## Task 1 — pure helpers (no database, no child processes)

- [x] `ParsePgVersion`, `ServerMajor`, `CheckClient` against fake `pg_dump` shell scripts (missing, older, equal, newer).
- [x] `ConnEnv(dsn)`: URL and key=value forms to `PG*` variables, `sslmode` kept, no password in the returned database name.
- [x] `Manifest` encode into a fixed slot and decode back; unknown format version rejected.
- [x] `checkEntry` table: accepts `git-repos/a/b.git/HEAD`; rejects `..`, absolute, `./`, double slash, symlink, hardlink, device, unknown top-level name.
- [x] `db.MigrationKnown`.

## Task 2 — repository copy and extraction

- [x] Copy order: in a bare repo, every `HEAD`/`config`/`packed-refs`/`refs/**` entry precedes the first `objects/**` entry; `objects/pack` is last; `.import-tmp`, `.readyz-*`, `*.lock` and `tmp_*` are skipped; `.deleted.*` copies are kept.
- [x] Concurrency: a real push through `gittransport` lands between the refs and the objects of the copy; every ref in the extracted copy resolves and the tree walks.
- [x] Extraction refuses bad archives (symlink, `..`, absolute) before the first file is written; extraction into a non-empty root is refused.

## Task 3 — `Create` and `Restore`

- [x] Integration (needs `TEST_DATABASE_DSN` plus `pg_dump`/`pg_restore` ≥ the server's major on `PATH`; skips otherwise): seed a source database and repos root, back up, restore into a new database and root, compare per-table row counts, refs and SHAs, go-git history walk, host key bytes and mode.
- [x] Refusals, each naming the reason and writing nothing: non-empty database, non-empty repos root, unknown format version, unknown migration, different host key without `--replace-host-key`.
- [x] Restore applies migrations newer than the backup (`db.Migrate` after `pg_restore`; checked by `Pending` being empty). No dedicated older-backup test: a fixture would hard-code a migration number.
- [x] Missing or too-old `pg_dump` fails with the needed version and leaves no output file.
- [x] Reconciliation report: row without directory, directory without row.

## Task 4 — CLI, image, docs

- [x] `backup --output`, `restore --input`, `--pg-dump`, `--pg-restore`, `--replace-host-key`; `-` for stdout/stdin.
- [x] `Dockerfile`: `postgresql18-client`; fix the SQLite comment. `docker run … pg_dump --version` reports 18.x.
- [x] `docs/deployment.md` "Backup and restore"; `docs/configuration.md` CLI entries and the `postgres_data` volume name.
- [x] `make lint`, `make test`, `make test-integration`; tick the ticket, set `Status: done`.

## Added during implementation

- [x] A `storage/` section for the local storage root (avatars), because the S3 avatar work has landed and Docker defaults to local storage.
