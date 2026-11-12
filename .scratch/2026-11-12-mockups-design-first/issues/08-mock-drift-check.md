# Cheap drift check for hand mocks

Created: 2026-11-12
Category: enhancement
Status: done

## Problem

Hand mocks copy classes from `.templ` files, so they drift (ticket 20 measured fidelity at about 8.3; the plan wants
9.5). `tests/mockgen-diff` compared a mock's tags, classes and attribute names with a live render, but only worked on
generated files and was removed with the generator.

## Scope

Decide whether a lighter check is worth having, and if so build the smallest one:

- Option A: a local script that compares a hand mock's class tokens with the live route's, with a per-route allow-list
  for sample data. It needs a seeded throwaway server (see `docs/testing.md`).
- Option B: no tooling; add a "mock matches the templ" line to the PR template and rely on review.

`git show acbbb688:tests/mockgen-diff/compare.mjs` has the normalising and comparing logic to adapt.

## Acceptance criteria

- [x] A decision between A and B, recorded here
- [x] ~~If A: documented in `docs/testing.md`, local only, not in CI~~

## Comments

2026-11-12 (maintainer): option B. Hand mocks carry sample data, so a class diff needs a per-route allow-list and a seeded
server for little gain. The PR template's mock line now says to copy classes from the templ. Reopen option A if the next
fidelity re-score (UI/UX ticket 22) still shows drift.
