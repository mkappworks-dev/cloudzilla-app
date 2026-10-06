# Avatars on activity surfaces

Created: 2026-10-06
Category: enhancement
Status: done
Spec: ../spec.md (§8 activity surfaces)

## What to build

Render uploaded avatars on these activity surfaces:

- comments
- issue meta
- pull detail: author, reviewers, collaborators and participants
- new pull and new issue
- discussions
- releases
- gists
- pulse
- contributors
- the sidebar's collaborators and assignees

Queries that join `users` for an author add `u.avatar_key`. Username lists get their keys through one batched `IN (...)` lookup. Commit-author sites keep initials.

## Acceptance criteria

- [x] Every activity surface listed in spec §8 renders the uploaded avatar, with initials when there is none.
- [x] Username lists resolve all their keys in one query per page.
- [x] `commits.templ`, `pr_commits.templ`, `commit.templ` and `repo.templ`'s latest commit still render initials.
- [x] A browser check shows the avatar next to a comment.

## Blocked by

- 03

## Comments
