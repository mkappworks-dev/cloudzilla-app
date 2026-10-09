# README editor and applied suggestions run the push side effects; docs

Created: 2026-10-08
Category: bug
Status: done
Parent: ../spec.md

## What to build

`SaveProfileReadme` and `ApplySuggestion` return a `RefUpdate`; their handlers call `AfterWebCommit`. Update `docs/code-browser.md`, `docs/webhooks.md` and `docs/git-transport.md`.

## Acceptance criteria

- [x] Saving the profile README and applying a suggestion fire the push side effects.
- [x] A suggestion applied to a PR's branch updates the PR's head SHA.
- [x] The docs no longer say web commits fire no side effects, and name `PushService`.

## Blocked by

02
