# Workflow parser: parse, validate, expand, evaluate

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: none
Parallel with: 01

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-18.2-workflow-parser` | `.worktrees/feat+phase-18.2-workflow-parser` | `feat(ci): workflow parser (18.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`. ADR 0004.

## Problem

Nothing can read a Workflow. Every later ticket (run creation, the runner, the UI) needs one validated structure and
line-numbered errors, and the parser is pure, so it can be finished and tested before any schema exists.

## Scope

- New package `internal/ci/workflow`: `Parse(path string, data []byte) (*Workflow, []Error)`. Errors carry file, line,
  column and a message naming the key.
- The supported subset from the spec's Implementation Decisions, including the `artifacts:` extension. Rejected keys
  (`uses`, `services`, `schedule`, `paths`, `pull_request_target`, `permissions`, `outputs`, `environment`, `defaults`,
  reusable workflows) and any unknown key each produce an error, never a silent skip.
- Job graph validation: unknown `needs` target, a cycle, duplicate job ids, empty `steps`.
- `strategy.matrix` expansion (axes, `include`, `exclude`) into concrete Jobs with stable names, failing when the Job
  count passes `MaxJobs` (a parameter, default 50) or steps pass `MaxSteps` (200).
- Expression parser and evaluator for `if:` and `${{ }}` with the contexts, functions and operators in the spec, plus a
  validation pass that rejects an unknown context or function at parse time. `secrets.*` is validated but never
  resolved here.
- Trigger matching helpers the run creator will call: does this push (branch or tag, with `branches` and `tags` globs)
  or pull request event match; the glob rules match GitHub's `*`, `**` and `!` behaviour for branches.
- `docs/ci.md` is started with the supported-keys table and the not-supported list (finished by ticket 07).

## Files (hotspots in bold)

- `internal/ci/workflow/*.go` (new), `internal/ci/workflow/testdata/` (new)
- `docs/ci.md` (new), `go.mod` only if `gopkg.in/yaml.v3` is not already a direct dependency

## Acceptance criteria

- [x] Every supported key has a table test; every rejected key asserts the exact message and line
- [x] A pasted real-world shell-only GitHub workflow parses; one with `uses:` fails at that line naming `uses`
- [x] A matrix of 7×8 fails with the job-limit message; a matrix with `include` and `exclude` expands to the expected
      names
- [x] A `needs` cycle and an unknown `needs` target each fail with the job ids
- [x] `if: ${{ failure() && github.ref == 'refs/heads/main' }}` evaluates against a fake context; an unknown function
      fails at parse time
- [x] Branch and tag glob matching has tests for `*`, `**`, `!` and a ref with a slash
- [x] The package imports nothing from `store`, `service` or `handler`
- [x] ~~Light and dark checks at 1440~~ (no UI)
- [x] ~~Standing mocks in `mockups/` updated~~ (no UI)
- [x] ~~Verification on a throwaway server~~ (pure package; its tests run without a database)
- [x] `make lint` and `go test ./...` pass

## Verification

`go test ./internal/ci/workflow/`.

## Comments

- Claude, 2026-11-13: done. The parser imports `go.yaml.in/yaml/v3` (already an indirect dependency, now direct).
  `go vet ./...` reports unkeyed-field warnings in generated `internal/view/pages/*_templ.go`, which this ticket does not
  touch; `go vet ./internal/ci/...`, `golangci-lint run ./internal/ci/...` and `go test ./...` pass.
