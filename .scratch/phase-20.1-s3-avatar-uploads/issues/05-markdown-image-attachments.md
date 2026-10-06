# Markdown image attachments on the storage layer

Created: 2026-10-06
Category: enhancement
Status: needs-triage
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

## Blocked by

- 01

## Comments
