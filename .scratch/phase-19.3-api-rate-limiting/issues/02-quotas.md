# Repository count and storage quotas

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 01
Branch: `feat/phase-19.3-quotas`, from `origin/main` once 01 has merged

Build "Design: quotas (ticket 02)" from [the spec](../spec.md).

## Acceptance criteria

See "02: quotas" in [the spec](../spec.md#02-quotas). Tick them there and here together.

- [ ] Every 02 criterion in the spec is met.
- [ ] Integration tests cover each creation path refused at the count quota, a push refused at the storage quota on both transports, and a delete-only push accepted.
