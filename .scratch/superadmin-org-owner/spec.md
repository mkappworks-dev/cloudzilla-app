# Superadmin adds an organization owner

Created: 2026-10-06
Category: enhancement
Status: done

## Problem

Superadmins have no organization override: org permission checks look only at `org_members`. When an org's only owner is suspended (see [../admin-user-management/spec.md](../admin-user-management/spec.md)), nobody can manage its members, settings or repos. An admin can't delete that owner either, because `DeleteUser` refuses a sole org owner (`ErrSoleOrgOwner`) and nobody can add a second owner.

## Proposed design

A superadmin action that makes a chosen user an owner of any organization, reachable from the admin user page's "Sole owner of" list. It needs the admin's password and 2FA code and writes an `admin.org.owner_add` audit entry. It grants nothing else: the superadmin doesn't become a member unless they pick themselves.

## Decisions

- Removing or demoting an org owner is out of scope. Adding an owner unblocks the stated case (the new owner can then demote or remove the suspended one, or an admin can delete that account), and a superadmin override over existing owners is a wider grant to take up separately.
- The target can be an existing member (promoted) or a non-member (added as owner). The ghost and suspended accounts are refused: a suspended owner can't manage anything.
- Granting to an existing owner is a no-op that still succeeds and audits.

## Already in the codebase

`OrgService.AddMember` and `UpdateMemberRole` need the requester to be an org owner, so a superadmin can't use them. `AdminUserService` has the confirmation, audit and "Sole owner of" list (`SoleOwnedOrgs`) to build on; there is no org override and no `admin.org.*` audit action.

## Comments

2026-10-08 (triage): ready-for-agent; split into `issues/01`, `issues/02`.
