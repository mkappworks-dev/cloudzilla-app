# Global API rate limits

Created: 2026-10-06
Category: enhancement
Status: done
Branch: `feat/phase-19.3-api-rate-limiting`

Build "Design: rate limits (ticket 01)" from [the spec](../spec.md).

## Acceptance criteria

See "01: rate limits" in [the spec](../spec.md#01-rate-limits). Tick them there and here together.

- [x] Every 01 criterion in the spec is met.
- [x] Unit tests cover the subject resolution, the path classifier, headers, refusals, exemptions and config loading.
- [x] A router-level test shows that a session, a PAT and a git Basic-auth PAT each land in the right bucket.
