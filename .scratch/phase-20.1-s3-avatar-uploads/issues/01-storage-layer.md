# Storage layer: local and S3-compatible backends

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Spec: ../spec.md (§1, §2, §10)

## What to build

A new `internal/storage` package with a `Backend` interface (`Put`, `Get`, `Delete`, `Exists`), `ErrNotFound`, and `ValidKey`. Two implementations:

- `LocalBackend`: objects live under `storage.local.root` and are accessed through an `os.Root`. `Put` writes a temp file and renames it into place.
- `S3Backend`: built on `aws-sdk-go-v2`. It supports a custom endpoint, a region, path-style addressing and a key prefix. Static keys are optional; without them the SDK's default credential chain applies. `RequestChecksumCalculation` and `ResponseChecksumValidation` are both set to `WhenRequired`.

`storage.New(config.StorageConfig)` picks the backend and is wired in `cmd/server/main.go`. Each `storage.*` key gets a viper default and a `CZ_STORAGE_*` env var. `docker-compose.yml` adds a `versitygw` service under the `s3` profile. The Docker image sets the local root to `/data/storage`, and `./storage` is gitignored.

## Acceptance criteria

- [ ] `storagetest.Run(t, Backend)` covers put/get/exists/delete, overwrite, `ErrNotFound`, deleting a missing key, invalid keys, and prefix isolation.
- [ ] The suite passes against `LocalBackend` on `t.TempDir()`.
- [ ] The suite passes against `S3Backend` pointed at an `httptest` fake S3 server. The fake asserts that SigV4 `Authorization` is present and that no `x-amz-checksum-*` header is sent.
- [ ] The suite also runs against a real server when the `TEST_S3_*` env vars are set.
- [ ] A traversal test shows no key escapes the local root.
- [ ] An unknown backend, or `s3` with no bucket, makes `storage.New` return a clear error, and startup fails.
- [ ] `docker compose --profile s3 up` starts versitygw with a bucket.

## Blocked by

None.

## Comments
