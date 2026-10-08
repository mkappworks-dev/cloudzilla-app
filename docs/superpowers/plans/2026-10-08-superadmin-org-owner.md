# Superadmin adds an organization owner — implementation plan

Spec: [.scratch/superadmin-org-owner/spec.md](../../../.scratch/superadmin-org-owner/spec.md). Tickets: `issues/01`, `issues/02`.

**Goal:** a superadmin can make any user an owner of any organization from the admin user page, with confirmation and an `admin.org.owner_add` audit entry.

**Architecture:** Store (`OrgStore.SetOwner`, one upsert) → Service (`OrgService.AdminAddOwner`) → Handler (`AdminAddOrgOwner`, `POST /api/admin/orgs/{org}/owners`) → Templ card on `/admin/users/{username}`.

## Task 1 — service and store (ticket 01)

- [ ] Test `TestOrgService_AdminAddOwner` (`internal/service/org_admin_owner_test.go`): non-member, member and owner targets end as owners; the admin doesn't join; suspended → `ErrUserSuspended`; unknown org, user and ghost → `sql.ErrNoRows`.
- [ ] `OrgStore.SetOwner`: `INSERT … ON CONFLICT (org_id, user_id) DO UPDATE SET role = 'owner'`. It never drops an owner, so no org lock or last-owner check.
- [ ] `OrgService.AdminAddOwner`: org by name, user by username (skips the ghost), refuse suspended, `SetOwner`.
- [ ] `model.AuditActionAdminOrgOwnerAdd`.

## Task 2 — endpoint, page, docs (ticket 02)

- [ ] Test `TestAdminOrgOwner_AddNeedsSuperadminAndPassword` (`internal/handler/admin_org_owner_handler_test.go`): 403 for a non-superadmin and for a wrong password with nothing changed; 409 suspended; 404 unknown user and org; success redirects to `from`, adds the owner, audits with the username, and leaves the admin out of the org; the user page has the form.
- [ ] Handler: superadmin check, `confirmAction`, service call, audit, redirect or `HX-Refresh`. Reuses `adminUserActionError`.
- [ ] Route in the `/api/admin` group.
- [ ] `adminUserOrgOwners` card under Actions, one form per sole-owned org, reusing `#admin-user-confirm`.
- [ ] `docs/access-control.md` (new section, matrix row, delete refusal) and `docs/organizations.md` (deletion, API table, service list).

## Out of scope

Removing or demoting an owner as a superadmin (see the spec's Decisions).
