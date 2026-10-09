# Grant org owner without being an owner

Created: 2026-10-08
Category: enhancement
Status: done

Parent: [../spec.md](../spec.md)

## What to build

`OrgService.AdminAddOwner(ctx, orgName, username)` makes a user an owner of any organization: a member is promoted, a non-member is added as owner, an existing owner is left alone. Backed by one upsert, `OrgStore.SetOwner`. The caller has already checked superadmin and confirmed.

## Acceptance criteria

- [x] A non-member becomes an owner; a member is promoted; an owner stays one without error.
- [x] Unknown org or user returns `sql.ErrNoRows`; a suspended user returns `ErrUserSuspended`; the ghost is not found.
- [x] The acting admin doesn't become a member.
- [x] `admin.org.owner_add` audit constant exists.
