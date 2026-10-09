# Markdown image attachments on the storage layer

Created: 2026-10-06
Category: enhancement
Status: done
Spec: ../spec.md (Out of scope)

## Problem

Users can't paste or drop images into issue, PR and comment markdown. The storage layer from 01 could hold them, but attachments raise questions avatars don't:

- **Access:** an image in a private repo's issue must not be readable by anyone who has its URL.
- **Data:** an attachments table that links each object to its repo and uploader.
- **Cleanup:** removing uploads that were never referenced, or whose comment was deleted.

## Open questions

- Should attachments be served through the repo's `CanRead` check, or through unguessable URLs?
- What size and type limits apply? Non-image files?
- Should the attachments be deleted when the repo is deleted?

## Acceptance criteria

- [x] Pasting or dropping an image into an issue, PR, discussion, release or milestone editor uploads it and inserts its markdown.
- [x] An attachment is readable only by viewers who pass the repo's `CanRead`, checked on every request; unreadable and missing look the same.
- [x] Images only (PNG, JPEG, GIF, WebP by content), at most 10 MB, stored as uploaded.
- [x] A purged repo's attachments are deleted; a soft-deleted repo's are kept for Restore.
- [x] An hourly sweep deletes unreferenced uploads after a 24-hour grace period, and those of repos that no longer exist.
- [x] `docs/storage.md` and `docs/api-reference.md` describe the layout, access rule and cleanup.

## Blocked by

- 01

## Comments

**Claude, 2026-10-08 (triage: needs-info).** Not ready for an agent: the three open questions are product decisions that change the data model and the serving path, and the spec doesn't settle them. Nothing on `origin/main` or in open branches covers this yet. Answers needed, with a recommendation for each:

1. **Access.** Recommend serving through the repo's `CanRead` check (`RepoService.CanRead`), not unguessable URLs. A leaked or pasted URL would otherwise expose a private repo's image forever, and a repo made private later would keep serving its old images. The cost is that attachments can't be cached `public` or served from a CDN, and each request does a repo lookup plus a permission check. Unguessable URLs are simpler but give up that guarantee.
2. **Limits.** Recommend images only (PNG, JPEG, GIF, WebP, typed from the bytes as in `avatar.Process`), at most 10 MB each, with no re-encoding, so animated GIFs survive. Decide whether EXIF should be stripped (privacy) and whether non-image files (zip, log, PDF) are wanted at all. Non-images need a `Content-Disposition: attachment` serving path and are a bigger decision.
3. **Repo deletion.** Recommend deleting the objects after the repo's database delete commits, as avatars do for users and orgs. Say if a retention window is wanted instead.

Also to confirm: orphan cleanup (uploads never referenced, or whose comment was deleted) as a sweep job on a grace period, rather than on every comment edit.

**Maintainer, 2026-10-08.** Accepted all recommendations above: serve through `CanRead`; images only (PNG, JPEG, GIF, WebP by content), 10 MB, no re-encoding; delete objects after the repo delete commits; orphan sweep on a grace period. Status: ready-for-agent.

**Claude, 2026-10-08 (implementation notes).** Where the build refines the accepted answers:

- **Repo deletion.** Repo delete is a soft delete with a 30-day Restore, so objects go at `PurgeExpired`, not at delete. Org and user deletes hard-delete repos by cascade; `attachments.repo_id` has no foreign key, so the hourly sweep finds those rows and removes them.
- **Wiki.** The wiki editor doesn't offer uploads: its pages live in git, where the sweep can't see a reference and would delete the image after 24 hours.
- **Cache.** Responses are `private, no-cache` with an ETag, not cached for a year, because access can change.
- **EXIF.** Stored as uploaded, per "no re-encoding", so GPS metadata stays in the original. Stripping it would need a lossless JPEG/PNG metadata rewrite; not done.
- **Not done:** per-user or per-repo upload quotas, and non-image files.
