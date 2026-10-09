# Repository count and storage quotas

Created: 2026-10-06
Category: enhancement
Status: done
Blocked by: 01
Branch: `feat/phase-19.3-api-quotas`

Build "Design: quotas (ticket 02)" from [the spec](../spec.md).

## Acceptance criteria

See "02: quotas" in [the spec](../spec.md#02-quotas). Tick them there and here together.

- [x] Every 02 criterion in the spec is met.
- [x] Integration tests cover each creation path refused at the count quota, a push refused at the storage quota on both transports, and a delete-only push accepted.
