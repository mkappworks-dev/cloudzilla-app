# CI research (2026-11-13)

Primary docs only. "n/f" means the page the agent reached did not cover it; it is not a claim that the feature is absent.

## Comparison

| | GitHub Actions | Gitea / Forgejo | GitLab CI | Bitbucket | Woodpecker | Drone |
| --- | --- | --- | --- | --- | --- | --- |
| File | `.github/workflows/*.yml` | `.gitea/workflows`, `.forgejo/workflows` (Forgejo falls back to `.github/workflows`) | one `.gitlab-ci.yml` plus `include` | one `bitbucket-pipelines.yml` | `.woodpecker/*.yaml` | one `.drone.yml` |
| Syntax | `jobs`, `needs`, `uses`, `${{ }}` | GitHub's, with a documented difference list | stages plus `needs` DAG, `rules` | steps | container steps, `depends_on` | container steps, `depends_on` |
| Runner | hosted ephemeral VMs, or self-hosted | act_runner / Forgejo Runner, scoped registration token, labels | `glrt-` token; Docker/Kubernetes executors | n/f | agent dials server over gRPC, shared secret | polling runners |
| Isolation | self-hosted "almost never for public repos" | container per workflow; host label has none | shell executor high risk; non-privileged Docker safe | n/f | container per step; privileged only for admin-trusted repos | ephemeral containers |
| Fork PRs | no secrets, read-only token, approval for first-timers | approval, no secrets | no protected variables or runners | do not trigger | approval gate, default for forks | secrets withheld unless opted in |
| Secrets | repo, org | user, org, repo | masked, protected, hidden | secured, masked | per-event and per-image limits | withheld from PRs |
| Artifacts | 90-day retention | server-stored; cache on runner | `expire_in`, 100 MB default | 1 GB, 14 days | n/f | n/f |
| Logs | search, download, delete | n/f | chunked append, 100 MB cap | n/f | 5000-line cap | n/f |
| UI | runs list, run page, cancel, re-run | Actions tab | list, graph, retry, cancel | history, rerun | n/f | n/f |

## Decisions (user, 2026-11-13)

- Format: GitHub-Actions subset in `.cloudzilla/workflows/*.yml`, tab named Actions; `uses:` and services rejected at
  parse time.
- Runner: separate `cz-runner` process, non-privileged Docker container per job, scoped registration token.
- Fork PRs: writer approval per run, no secrets, read-only per-run token.
