# Superadmin adds an organization owner

Created: 2026-10-06
Category: enhancement
Status: needs-triage

## Problem

Superadmins have no organization override: org permission checks look only at `org_members`. When an org's only owner is suspended (see [../admin-user-management/spec.md](../admin-user-management/spec.md)), nobody can manage its members, settings or repos. An admin can't delete that owner either, because `DeleteUser` refuses a sole org owner (`ErrSoleOrgOwner`) and nobody can add a second owner.

## Proposed design

A superadmin action that makes a chosen user an owner of any organization, reachable from the admin user page's "Sole owner of" list. It needs the admin's password and 2FA code and writes an `admin.org.owner_add` audit entry. It grants nothing else: the superadmin doesn't become a member unless they pick themselves.

## Open questions

- Should a superadmin also be able to remove or demote an org owner?
