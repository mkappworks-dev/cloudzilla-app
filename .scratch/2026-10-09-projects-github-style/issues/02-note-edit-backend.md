# Edit a note card

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Store method, `ProjectService.UpdateCardNote(ctx, projectID, cardID, userID, note)` and handler route in `internal/router/router.go`.
- [ ] Only note cards are editable; issue/PR cards are rejected. Empty text is rejected.
- [ ] Requires write access, same as `DeleteCard`; service and router tests cover it.
