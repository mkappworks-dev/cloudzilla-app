# CI workflows

A Workflow is a YAML file at `.cloudzilla/workflows/*.yml` or `*.yaml` in a Repository, in a documented subset of the
GitHub Actions syntax ([ADR 0004](./adr/0004-workflow-format-is-a-github-actions-subset.md)). The parser is
`internal/ci/workflow`: `Parse(path, data)` returns a validated `Workflow` with matrix jobs already expanded, or
line-numbered `Error`s. It is pure and knows nothing about the database.

## Supported keys

| Level                                | Keys                                                                                                                     |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| Workflow                             | `name`, `on`, `env`, `concurrency` (`group`, `cancel-in-progress`), `jobs`                                               |
| `on`                                 | `push` (`branches`, `tags`), `pull_request` (`types`: `opened`, `reopened`, `synchronize`), `workflow_dispatch`          |
| Job                                  | `name`, `runs-on`, `needs`, `if`, `env`, `container` (image string), `timeout-minutes`, `strategy`, `steps`, `artifacts` |
| `strategy`                           | `matrix` (axes, `include`, `exclude`), `fail-fast`                                                                       |
| Step                                 | `name`, `run`, `env`, `if`, `timeout-minutes`, `working-directory`, `shell` (`sh` or `bash`)                             |
| `artifacts` (a Cloudzilla extension) | a list of `{name, path}` on the Job                                                                                      |

`on: push` alone, or a list of events, is accepted. `pull_request` defaults to all three types. `workflow_dispatch`
takes a ref and no inputs. `timeout-minutes` defaults to 60 and may not pass 360.

`branches` and `tags` take GitHub's patterns: `*` matches within one path segment, `**` crosses `/`, `?` and `+` apply
to the previous character, and a leading `!` removes earlier matches. A list needs one pattern without `!`. With neither
key, every branch and tag matches; with only one, the other kind of ref does not.

## Expressions

`${{ }}` is accepted in string values and `if:` takes an expression with or without it.

- Contexts: `github` (`sha`, `ref`, `ref_name`, `event_name`, `actor`, `repository`, `run_number`), `env`, `secrets`,
  `matrix`, `needs.<id>.result`.
- Functions: `success()`, `failure()`, `always()`, `cancelled()`, `contains`, `startsWith`, `endsWith`, `format`.
- Operators: `==`, `!=`, `&&`, `||`, `!` and parentheses. String comparison ignores case.
- An `if:` with no status function is treated as `success() && (…)`; with no `if:`, as `success()`.
- `secrets.*` is checked at parse time and left in the string for the runner; the server never resolves it.

An unknown context, function or operator fails the parse.

## Limits

50 Jobs per Run after matrix expansion and 200 Steps per Run; a matrix over the Job limit fails with the count.

## Not supported

Each of these fails the parse with the file, line and key, as does any key not listed above:

`uses` (actions and reusable workflows), `services`, `schedule`, `paths` filters, `pull_request_target`, `permissions`,
`outputs`, `environment`, `defaults`, `workflow_call`, `workflow_dispatch` inputs.

`actions/upload-artifact` is a `uses:` step, so Cloudzilla has the job-level `artifacts:` list instead.
