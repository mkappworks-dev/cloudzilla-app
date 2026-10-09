# Card details API

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 02

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] `POST /cards` accepts title, description, due date, assignees, labels and an optional link; the composer's title-only and link-only forms still work.
- [ ] `PATCH /cards/{cardID}/details` updates fields and replaces assignee and label sets in one transaction.
- [ ] Rejected with the right status: empty title on a note card, assignee who is not the owner or a collaborator, label from another repo, link to another repo, card from another project, anonymous or read-only caller.
- [ ] Service, store and router tests cover each rejection and the success path.
