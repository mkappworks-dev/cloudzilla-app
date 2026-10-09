# Create a pull mirror from the import form and API

Created: 2026-10-06
Category: enhancement
Status: done
Blocked by: 02, 03

Spec: [../spec.md](../spec.md#pull-mirrors)

## What

- The import form (`repo_import.templ`) gains a "Keep this repository in sync" checkbox and an interval field, defaulting to `mirror.default_interval`. When the box is checked, the credential hint changes from "Used once… not stored" to "Stored encrypted to keep the mirror in sync." These controls are hidden when `mirror.enabled` is false. The agreed layout is frame 1 of the [mockups](https://claude.ai/artifact/1qdQ3E9jfTCzmijzWhWRgq): a checkbox card directly under the URL, with an interval picker offering fixed choices (10 minutes, 1 hour, 8 hours, 1 day, 1 week), filtered to at least `mirror.min_interval`.
- `POST /api/imports` gains `mirror` (bool) and `mirror_interval` (a duration string). The interval must be at least `min_interval` and at most 30 days.
- With `mirror` set and a token given but no secret key configured, the request is refused with "Mirroring with credentials needs security.secret_key."
- On a successful publish, the repo row and the mirror row are written in one step, with the token sealed. The publish also runs `IndexRepo` and `ParseAndStore` once.
- Audit: `repo.mirror.create`.

## Acceptance criteria

- [x] A mirror import creates a repo with `IsMirror` true and the next sync scheduled.
- [x] A plain import is unchanged.
- [x] Interval bounds and the missing-key case are refused with clear messages, in both the form and the API.
- [x] The stored token is ciphertext, and opening it gives back the original.

## Comments

**Claude, 2026-10-06:**
- The mirror row is written by a callback in `RepoService.createFromImport`, right after the repo row. If it fails, the import is abandoned the same way as when the rename fails.
- The interval list is the presets that fit `mirror.min_interval`, plus the instance's default if it isn't a preset.
- A mirror import records `repo.mirror.create`, with the requested interval, instead of `repo.import`.
