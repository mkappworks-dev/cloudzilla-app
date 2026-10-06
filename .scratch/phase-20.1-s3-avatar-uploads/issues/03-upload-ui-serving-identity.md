# Upload UI, /avatars serving and identity surfaces

Created: 2026-10-06
Category: enhancement
Status: done
Spec: ../spec.md (§4 API, §7, §8 identity surfaces, §9, §10 docs)

## What to build

- Routes:
  - `POST /settings/avatar` and `/settings/avatar/delete`.
  - `POST /orgs/{org}/settings/avatar` and `.../delete`, outside the `/api/orgs` 1 MB group.
  - `GET /avatars/*`.
- The upload routes use `MaxBodySize(2 MB + 64 KB)`. Errors map to status codes as follows:
  - 413 for too large
  - 422 for a bad image, with an inline message
  - 403 for a non-owner
  - 404 for an unknown org
- Org avatar changes go to the audit log.
- `/avatars/*`:
  - The key must match the strict pattern.
  - Headers: immutable cache, `ETag`, `nosniff`, sandbox CSP.
  - A matching `If-None-Match` gives 304, `ErrNotFound` gives 404, and other backend errors give 502.
- One avatar component that renders an `<img>`, or initials when there is no key. Plus `view.AvatarURL`.
- `basePage` reads the signed-in user's `avatar_key` in the same lookup that already runs, so the nav avatar costs no extra query.
- Identity surfaces from §8 render the image.
- The JSON API's `avatar_url` becomes the absolute uploaded URL when a key is set.
- The settings control is the picked "buttons row": avatar, "Upload new picture", and "Remove" (only when set), with help text and the error slot below.
- Docs:
  - New `docs/storage.md`.
  - `configuration.md`, `deployment.md` and `api-reference.md`.
  - ROADMAP 20.1.
  - CLAUDE.md subsystem link.

## Acceptance criteria

- [x] Upload, replace and remove work from user settings and org settings, and a non-owner gets 403.
- [x] 413 over the cap, including on a cookie-authenticated multipart post without `X-CSRF-Token`.
- [x] 422 for a bad image, and 401 when signed out.
- [x] `/avatars/*` sends the cache headers, gives 304 on `If-None-Match`, and gives 404 on a malformed or unknown key.
- [x] Nav, settings, user profile, org pages, org members, search and stargazers render the `<img>` with the right `src`.
- [x] The API's `avatar_url` reflects the uploaded avatar.
- [x] The docs are updated.

## Blocked by

- 02

## Comments
