# CI pipelines

Created: 2026-11-06
Category: enhancement
Status: ready-for-human
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/phase-18.1-ci-runner` | `.worktrees/feat+phase-18.1-ci-runner` | `feat: ci pipelines (18.1–18.3)` | 3 | L | Features +0.6 |

Mock: `../2026-11-06-ci-pipelines/`

## Problem

Roadmap phase 18.1–18.3 is planned and has no UI.

## Scope

- `/grill-with-docs`, `/to-spec` and `/to-tickets` in `../2026-11-06-ci-pipelines/`.
- Decided 2026-11-06: built-in CI gets its own repo tab, **Actions**, at `/{owner}/{repo}/actions` and `/actions/runs/{n}`, replacing today's repo-level Checks tab; external commit statuses stay reachable as a filter there. The PR **Checks** tab stays the results view for built-in and external CI.
- Rework the mock before the spec: move the runs list from the Checks tab into an Actions tab.
- Still open: workflow format. If it stays Cloudzilla's own syntax rather than GitHub-Actions-compatible, document the difference prominently (or name the tab Pipelines).
- Other decisions from the mock README: multiple workflow files under `.cloudzilla/workflows/`; job `needs`, steps, artifacts tables.

## Files

- per its spec

## Acceptance criteria

- [ ] Spec and tickets exist
- [ ] All its tickets done
- [ ] ROADMAP status updated

## Verification

Per its spec.

## Comments

2026-11-06 (triage): ready-for-human; needs a spec first; Actions tab decided, workflow format still open.
