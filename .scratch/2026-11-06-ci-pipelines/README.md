# CI mocks (ROADMAP 18.1-18.3)

The mocks are in `mockups/`: `actions.html`, `actions_run.html`, `pr_checks.html`, `repo_settings_ci.html`,
`admin_runners.html`, `actions_status_icons.html`; serve with `make mockups`. Decisions live in [spec.md](./spec.md);
the repo tab is **Actions** (`/actions`, `/actions/runs/{n}`) and the PR **Checks** tab stays as the shared results view.

## Pages and states

Every page takes `?state=` (the dashed bar's buttons); `actions.html`, `actions_run.html`, `pr_checks.html` and
`repo_settings_ci.html` also take `?role=`.

- `actions.html`: runs list. States `runs`, `waiting` (fork PR run, Approve for writers), `no-runner`, `statuses`
  (external CI), `yaml-error`, `no-match`, `empty`, `dispatch` (Run workflow popover). `?role=reader|writer|admin`.
- `actions_run.html`: run detail with Jobs, Steps, logs and Artifacts. States `queued`, `running`, `failed`, `success`,
  `cancelled`, `yaml-error`, `waiting`, `no-runner`, `runner-lost`, `truncated`; also `?q=` and `?ts=1`.
- `pr_checks.html`: PR Checks tab, Pull requests highlighted. States `failing`, `running`, `waiting`.
- `repo_settings_ci.html`: Settings, CI/CD. States `default`, `ci-off`, `new-runner`, `no-secret-key`.
- `admin_runners.html`: Admin, Runners. States `runners`, `empty`, `new-runner`.
- `actions_status_icons.html`: the status icon in a commit-list row, the PR header and the file tree, for success,
  failure, running and none. All icons come from `actions_status_icons.js`.
