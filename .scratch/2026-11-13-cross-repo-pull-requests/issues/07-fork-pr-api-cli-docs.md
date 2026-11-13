# API, CLI, webhooks, notifications and docs

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 04

## Problem

See [spec.md](../spec.md). Scripts, the `cz` CLI, webhooks and notifications handle fork PRs, and the docs describe them.

## Acceptance criteria

- [x] `cz pr create --head-repo owner/name`.
- [x] The `pull_request` webhook payload gains `head_repo` (`id`, `full_name`, `fork`); events and notifications fire on the base only (they are built from the base repository, so nothing needed to change).
- [x] `docs/pr-merge.md`, `docs/git-transport.md`, `docs/webhooks.md`, `docs/api-reference.md`, `docs/cli.md` and `docs/ROADMAP.md` updated and run through Prettier (not `.scratch/`).
