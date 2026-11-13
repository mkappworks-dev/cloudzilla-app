# 4. The workflow format is a GitHub-Actions subset

Status: accepted, 2026-11-13

## Context

Gitea and Forgejo chose GitHub's syntax so users can bring existing workflows; GitLab, Drone and Woodpecker have their
own. Cloudzilla has a GitHub-style UI and the ticket named the tab Actions. An own syntax would be free to design but
would make every user relearn it.

## Decision

Workflows are YAML under `.cloudzilla/workflows/` in a documented subset of the GitHub-Actions syntax: shell steps,
`needs`, `if`, `matrix`, `env`, `timeout-minutes`, `concurrency` and a small expression language. A key outside the
subset, `uses:` and `services` included, fails the parse with its file and line rather than being ignored. The one
deliberate extension is a job-level `artifacts:` list, because `actions/upload-artifact` is a `uses:` step. The docs
list every difference.

## Considered options

An own syntax with the tab named Pipelines was rejected for the relearning cost. Reading `.github/workflows` as a
fallback was rejected because real GitHub workflows mostly use `uses:`, so they would fail on first run. Ignoring
unknown keys was rejected because a green run would then not prove the steps it skipped.
