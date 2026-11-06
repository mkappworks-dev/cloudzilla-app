# Commit the UI/UX plan and feature mocks

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/uiux-plan-and-mocks` | `.worktrees/tech+uiux-plan-and-mocks` | `tech(scratch): desktop UI/UX plan and feature mocks` | 0 | S | none (unblocks every other ticket) |

## Problem

The plan, audit and five feature mocks exist only as untracked files in the main checkout. Worktrees branch from `origin/main`, so no other session can see them.

## Scope

- Commit `.scratch/2026-11-06-uiux-desktop-9.5/` and the five `.scratch/2026-11-06-*` mock folders.
- Decide whether compiled `mock.css` files (~125 KB each) and `shots/` PNGs are committed or regenerated; if not, add a README line on how to rebuild them (`bin/tailwindcss -i <dir>/mock-input.css -o <dir>/mock.css`) and gitignore them.

## Files

- `.scratch/2026-11-06-*/**`

## Acceptance criteria

- [x] Plan, audit and mocks are on `origin/main`
- [x] Each mock opens from a static server at the repo root

## Verification

`git ls-tree origin/main .scratch/` lists the six folders.

## Comments

2026-11-06 (triage): done in the branch that commits it. Compiled `mock.css` and `shots/` are gitignored; rebuild per spec.md.
