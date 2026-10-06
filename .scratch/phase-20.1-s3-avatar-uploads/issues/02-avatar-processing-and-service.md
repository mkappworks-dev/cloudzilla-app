# Avatar processing, migration and AvatarService

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Spec: ../spec.md (§3–§6)

## What to build

- A migration (the next free number) adding `avatar_key TEXT NOT NULL DEFAULT ''` to `users` and `organizations`. `model.User.AvatarKey` and `model.Org.AvatarKey` are scanned wherever those rows load.
- `internal/avatar.Process`:
  - 2 MB cap.
  - The type comes from the bytes: PNG, JPEG, GIF or WebP.
  - A header-only dimension check rejects anything over 4096 px a side or 16 MP.
  - Centre-crop, then CatmullRom scale to at most 460 px.
  - Re-encode as PNG when any pixel has alpha, otherwise JPEG at quality 90.
  - Returns the bytes, the extension and the SHA-256.
- `AvatarService` with Set/Remove for users and orgs, in the spec's order:
  1. Put the new object.
  2. Swap the key under `FOR UPDATE`.
  3. Delete the old object, unless the swap failed, in which case delete the new one.
- Org methods return `ErrNotOrgOwner` for anyone who isn't an owner.
- `UserService.DeleteUser` and `OrgService.Delete` delete the avatar object after the delete commits.

## Acceptance criteria

- [ ] Processing accepts PNG, JPEG, GIF and WebP.
- [ ] Processing rejects SVG, HTML, a PNG renamed `.svg`, and SVG declared as `image/png`.
- [ ] Processing rejects a file declaring 65535 × 65535 before decode, and a 2 MB + 1 byte input.
- [ ] EXIF GPS is gone from the output, alpha gives PNG and opaque gives JPEG, and output is at most 460 px, square and never upscaled.
- [ ] Service integration tests: upload, replace, the same image twice, remove, a failed key update, user and org deletion, and a non-owner refused.

## Blocked by

- 01

## Comments
