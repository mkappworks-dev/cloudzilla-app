# Admin delete

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: 03, 05

## What to build

`POST /api/admin/users/{username}/delete`: confirmed like the others and also requires typing the username. Runs `UserService.DeleteUser` (personal repos go, contributions pass to the ghost). Refused on self, for a sole organization owner (naming the orgs), and when it would leave no active superadmin. Audit `admin.user.delete` with the email. On success, redirect (or `HX-Redirect`) to `/admin/users`. The delete card on the user page uses the danger-zone styling from repo settings.

Document the new routes in `docs/api-reference.md` and the endpoint matrix and instance roles in `docs/access-control.md`.

## Acceptance criteria

- [x] Admin delete removes the account like self-service deletion, and refuses a sole organization owner.
- [x] `docs/access-control.md` (endpoint matrix, instance roles, "Suspended accounts") and `docs/api-reference.md` describe the new routes and behaviour.

## Tests

- Service: deleting a sole org owner returns `ErrSoleOrgOwner`; a mistyped username is refused; a plain user is deleted and audited.
