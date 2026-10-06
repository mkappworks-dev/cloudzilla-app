# Document repository mirrors

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03, 04, 05, 06, 07, 08

## What

- New `docs/repo-mirrors.md`, covering:
  - creating a mirror
  - sync behaviour and its side effects
  - read-only rules
  - credentials and `security.secret_key`
  - the scheduler and multi-instance safety
  - limits
- It is linked from `CLAUDE.md` under Subsystem docs.
- `docs/configuration.md` gets the `mirror.*` and `security.secret_key` rows.
- `docs/api-reference.md` gets the mirror endpoints and the new `/api/imports` fields.
- `docs/repo-import.md` points to mirrors.

## Acceptance criteria

- [ ] Every config key and endpoint added by tickets 02–08 is documented.
