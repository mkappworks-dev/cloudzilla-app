# CI pipelines mock (ROADMAP 18.1–18.3)

Shows built-in pipeline runs inside the existing **Checks** tab, not in a new Actions tab. `.scratch/2026-10-06-actions-mock-runs` made
`/actions` 404 and named the tab Checks, so the mock keeps one place for CI: "Pipelines" is the default view and "Commit statuses" is
today's `pages/checks.templ`. Classes come from `checks.templ`, `pr_checks.templ` (`checkRowIcon`, banner), `table`,
`tabs`, `accordion`, `alert`, `dialog`, `dropdown_menu`, the blob line table and the release asset rows.

Open with `python3 -m http.server` from the repo root at `/.scratch/2026-11-06-ci-pipelines/mock.html`. Rebuild CSS with
`bin/tailwindcss -i .scratch/2026-11-06-ci-pipelines/mock-input.css -o .scratch/2026-11-06-ci-pipelines/mock.css`.
Every page takes `?state=` (the dashed bar's buttons). Screenshots go in `shots/` (gitignored, not committed).

## Pages and states

- `mock.html`: runs list. Each run shows status, workflow and run number, trigger (push, pull request, manual), branch,
  commit, duration and actor. Filters for workflow, branch, status and trigger; a "Run workflow" popover with workflow
  and ref selects. States: `runs`, `yaml-error` (alert plus an "Invalid workflow" row), `no-match`, `empty` (how to add
  a workflow file), `dispatch`.
- `run.html`: run detail. The aside lists jobs and run metadata. The main column has a stage graph, then the selected
  job's steps as `<details>` sections, each with a log table: line anchors, a timestamps toggle, search with a match
  count, token-coloured "ANSI" spans and a "Download log" link. Artifacts follow. Re-run (failed or all jobs) and Cancel
  (confirm dialog). States: `queued`, `running` (lines append every 1.2 s, elapsed time ticks), `failed` (failing step
  open), `success`, `cancelled`, `yaml-error` (offending line highlighted with the parse message under it). Takes `?q=`
  and `?ts=1` too.
- `pr_checks.html`: PR Checks tab with a pipeline card (run header, "View run →", one row per job, linked to the run)
  above an "Other statuses" card for external CI. States: `failing`, `running`.

## ROADMAP vs mock

| ROADMAP proposes                                                 | Mock assumes                                                                                                                                               |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| One `.cloudzilla-ci.yml`, path in `pipeline_configs.yaml_path`   | Several files under `.cloudzilla/workflows/*.yml`, each with a `name:`; the run list filters by workflow                                                   |
| `pipelines` (status, branch, sha, triggered_by)                  | Also needs `workflow_path`, `number` (per repo), `event` (`push`/`pull_request`/`manual`), `pull_id`, `attempt`, `cancelled_by`, and a `queued` status     |
| `pipeline_jobs` (name, status, `log_output TEXT`)                | Also `needs` (for the graph), `runner`, and steps: a `pipeline_steps` table (name, status, timings, log offset), or step markers in the log                 |
| No artifacts                                                     | `pipeline_artifacts` (name, size, expiry) stored through the storage `Backend`                                                                             |
| `GET /{owner}/{repo}/actions`, `/actions/{id}`                   | `/{owner}/{repo}/checks` (pipelines), `/checks/statuses` (today's page), `/checks/runs/{number}`, `POST …/runs/{number}/cancel` and `…/rerun`, `POST …/checks/dispatch` |
| `on:` not specified                                              | `on: [push, pull_request, manual]`; manual runs pick a ref                                                                                                 |
| Runner reports through the status API                            | One status per job, context `ci/<job>`, `target_url` the run page; the PR tab groups them under their run                                                  |
| Live log via `hx-trigger="every 2s"`                             | Same, polling only the running step's tail by line offset                                                                                                  |

## Open questions

- Grouping job statuses under their run on the PR tab needs the status to point at the run (a `pipeline_id` column on
  `commit_statuses`, or matching `target_url`). Without that the tab shows them as flat rows.
- New statuses `queued`, `cancelled` and `skipped` need icons. The mock uses a muted clock, a slashed circle and a dash,
  with the pending dot pulsing for running: extend `checkRowIcon`, or add a `components.RunStatusIcon`?
- `log_output TEXT` on the job row doesn't fit live tailing or big logs. Store logs as objects and keep offsets?
- Can a failed run have no jobs (YAML error) and still post a failing status, so branch protection sees it?
- The "Run workflow" popover uses a plain `<select>` for the ref because the `RefPicker` popover can't nest in another
  popover. Is that acceptable?
- Settings → CI/CD (18.2) is linked from the empty state but not drawn.
- `uses:` steps are rejected; is "shell commands only" the v1 contract?
