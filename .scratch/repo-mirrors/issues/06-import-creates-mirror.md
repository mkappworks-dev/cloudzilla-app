# Create a pull mirror from the import form and API

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

- The import form (`repo_import.templ`) gains a "Keep this repository in sync" checkbox and an interval field, defaulting to `mirror.default_interval`. When the box is checked, the credential hint changes from "Used once… not stored" to "Stored encrypted to keep the mirror in sync." These controls are hidden when `mirror.enabled` is false. Agree the layout with the maintainer before building it.
- `POST /api/imports` gains `mirror` (bool) and `mirror_interval` (a duration string). The interval must be at least `min_interval` and at most 30 days.
- With `mirror` set and a token given but no secret key configured, the request is refused with "Mirroring with credentials needs security.secret_key."
- On a successful publish, the repo row and the mirror row are written in one step, with the token sealed. The publish also runs `IndexRepo` and `ParseAndStore` once.
- Audit: `repo.mirror.create`.

## Acceptance criteria

- [ ] A mirror import creates a repo with `IsMirror` true and the next sync scheduled.
- [ ] A plain import is unchanged.
- [ ] Interval bounds and the missing-key case are refused with clear messages, in both the form and the API.
- [ ] The stored token is ciphertext, and opening it gives back the original.
