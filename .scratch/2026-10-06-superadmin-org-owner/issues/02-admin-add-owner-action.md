# Add-owner action on the admin user page

Created: 2026-10-08
Category: enhancement
Status: done
Blocked by: 01

Parent: [../spec.md](../spec.md)

## What to build

`POST /api/admin/orgs/{org}/owners` (`username`, plus the admin's password and 2FA code) calls `AdminAddOwner` and writes `admin.org.owner_add` with target the org. The admin user page's "Sole owner of" list shows each org with a username field and an "Add owner" button. Docs updated.

## Acceptance criteria

- [x] Non-superadmin gets 403; a wrong password changes nothing; no audit entry on refusal.
- [x] Success adds the owner, audits (`org` target, `username` in metadata) and reloads the page.
- [x] Refusals map to 404 (org or user), 409 (suspended).
- [x] `docs/access-control.md` and `docs/organizations.md` describe it; the "Sole owner" notices point to it.
