# API, CLI, webhooks, notifications and docs

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 04

## Problem

See [spec.md](../spec.md). Scripts, the `cz` CLI, webhooks and notifications handle fork PRs, and the docs describe them.

## Acceptance criteria

- [ ] `cz pr create --head-repo owner/name`.
- [ ] The `pull_request` webhook payload gains `head_repo` (`id`, `full_name`, `fork`); events and notifications fire on the base only.
- [ ] `docs/pr-merge.md`, `docs/git-transport.md`, `docs/webhooks.md`, `docs/api-reference.md`, `docs/cli.md` and `docs/ROADMAP.md` updated and run through Prettier (not `.scratch/`).
