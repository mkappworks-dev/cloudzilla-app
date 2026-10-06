# `cloudzilla-cli backup` and `restore`

Created: 2026-10-06
Category: enhancement
Status: needs-triage
Spec: [../spec.md](../spec.md) (Backup and restore)

The spec's open questions 3 (form), 4 (hot or cold) and 7 (testing the restore) decide the shape of this issue. It's written for the recommended answers:

- a CLI command that wraps `pg_dump`;
- hot capture in a safe order;
- a round-trip test under `make test-integration`.

## What to build

**`cloudzilla-cli backup --output <path|->`**

- **Archive:** one uncompressed tar, file mode 0600. Entries, in order:
  1. `cloudzilla-backup.json`, the manifest: format version, Cloudzilla version, created-at, newest applied migration, `pg_dump` version, and per-section counts and bytes;
  2. `database.pgdump`, from `pg_dump --format=custom --no-owner --no-privileges`;
  3. `git-repos/…`;
  4. `ssh_host_key`, when the file exists.
- **Capture order:** the database dump first, then each repository with refs (`HEAD`, `config`, `packed-refs`, `refs/`) before `objects/`.
- **Skipped:** `.import-tmp/` and `.readyz-*`. `.deleted.*` copies are kept.
- **Client check:** fails before writing anything when `pg_dump` is missing, or older than the server's major version (from `SHOW server_version_num`).

**`cloudzilla-cli restore --input <path|->`**

It refuses to start unless:

- the database has no `schema_migrations` table;
- `git.repos_root` is empty or missing;
- the manifest's format version is known;
- the backup's newest migration is embedded in this binary.

Then it:

1. Runs `pg_restore --no-owner --no-privileges --exit-on-error --single-transaction`.
2. Extracts `git-repos/`, accepting only regular files and directories whose cleaned paths stay inside the root. Absolute paths, `..`, symlinks and devices are rejected.
3. Writes the host key, stopping if a different key exists unless `--replace-host-key` is given.
4. Applies newer migrations.
5. Prints a reconciliation report: repository rows without a directory, and directories without a row.

**Flags.** `--pg-dump` and `--pg-restore` override the binaries found on `PATH`.

**Image.** Add `postgresql18-client` to the runtime stage of the `Dockerfile`. Fix the stale "SQLite DB" comment there.

**Docs.**

- `docs/deployment.md` gets a "Backup and restore" section:
  - a nightly cron example using `docker compose exec`;
  - the stop-the-server variant for a fully consistent copy;
  - "don't run `gc` during a backup";
  - the archive holds secrets, so store it encrypted;
  - restore needs the same `auth.jwt_secret`, or everyone is signed out;
  - a restore runbook (empty database, empty volume, `restore`, start).
- `docs/configuration.md` gets CLI reference entries for both commands, and fixes the Postgres volume name (`postgres_data`, not `cloudzilla_pg_data`).

**Object storage.** Leave room for an extra section, but don't build one. When the S3 avatar work lands, a local avatar directory becomes a section. An S3 backend is recorded in the manifest, and `restore` warns about it.

## Acceptance criteria

- [ ] Backup from a seeded instance, then restore into a fresh database and an empty repos root. Every table has the same row count, and every repository has the same refs pointing at the same SHAs.
- [ ] Every restored repository opens with go-git and walks its HEAD history without missing objects.
- [ ] The host key file is restored byte-for-byte with mode 0600.
- [ ] `restore` refuses a non-empty database, a non-empty repos root, an unknown format version, and a backup with a migration this binary doesn't have. Each refusal message names the reason, and nothing is written.
- [ ] A tar entry with `..`, an absolute path or a symlink is rejected before anything is extracted.
- [ ] A backup taken while a push is in flight restores to a state where every ref resolves.
- [ ] `backup` with no `pg_dump` on `PATH`, or one older than the server, fails with a message naming the needed version, and leaves no partial output file.
- [ ] The Docker image has `pg_dump --version` reporting 18.x.

## Tests

- **Integration**, needing `TEST_DATABASE_DSN` and `pg_dump`/`pg_restore` 18 on `PATH`; it skips otherwise:
  - create a source database and a target database (`CREATE DATABASE` with unique names, dropped in cleanup);
  - fill the source with `internal/seed` at a small size;
  - back up, restore, compare.
- **Concurrency.** A test pushes through `gittransport` while the repository copy runs, and checks that the copy's refs all resolve.
- **Unit:** tar path validation, the manifest round trip, and the `pg_dump` version parse.
- **CI.** CI's `go test ./...` has no Postgres, so the round trip runs only under `make test-integration`, unless open question 7 adds a CI job. The PR states which ran.

## Files

- `cmd/cloudzilla/` (new `backup.go`, `restore.go`, registered in `main.go`)
- a new `internal/backup/` package for the archive and capture order
- `internal/db/migrate.go` (shares `db.Pending` from issue 01, or adds it if 03 lands first)
- `Dockerfile`
- `docs/deployment.md`
- `docs/configuration.md`
