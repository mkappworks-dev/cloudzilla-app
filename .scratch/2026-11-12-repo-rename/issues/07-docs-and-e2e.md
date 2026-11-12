# Docs, ROADMAP and end-to-end check

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03, 04, 05, 06

| Branch                 | Worktree                          | PR title                                        | Effort |
| ---------------------- | --------------------------------- | ----------------------------------------------- | ------ |
| `tech/repo-rename-docs` | `.worktrees/tech+repo-rename-docs` | `docs(repos): repository rename and redirects` | S      |

## Problem

See [spec.md](../spec.md). The behaviour has to be documented where the subsystems are, and checked once end to end.

## Scope

- New `docs/repo-rename.md` (or a section in `docs/access-control.md` if smaller): who may rename, the alias rules, the
  redirect table, the manual recovery for a crash between the dir move and the commit, and the `repository.id` note for
  webhook receivers. Link it from `CLAUDE.md`'s subsystem list.
- Update `docs/git-transport.md` (aliases for HTTP and SSH, extended "looks missing" table), `docs/wiki.md` (moves with
  rename), `docs/webhooks.md` (`full_name` changes, `id` is stable), `docs/repo-mirrors.md` (rename refused during a
  sync), `docs/api-reference.md`, `docs/cli.md` if `cz` gains a rename command (otherwise note it can't).
- `docs/ROADMAP.md`: status for repository rename; set ticket 15 `Status: done` in the uiux folder and tick its
  criteria once 02–06 are `done`.
- End-to-end run per the spec's Verification section; record the commands and result in the PR.
- `npm run fmt` on the docs.

## Files

- `docs/**`, `CLAUDE.md`, `docs/ROADMAP.md`, `.scratch/2026-11-06-uiux-desktop-9.5/issues/15-repo-rename.md`

## Acceptance criteria

- [ ] Every doc above updated; `npm run fmt:check` passes
- [ ] End-to-end run done on a throwaway server: rename, transfer, delete, restore, reuse the old name, HTTP and SSH git
- [ ] Ticket 15 marked `done` and ROADMAP updated

## Verification

Throwaway server and scratch DB as in the spec; paste the git transcripts into the PR.

## Comments
