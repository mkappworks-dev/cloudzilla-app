# CI workflows

A Workflow is a YAML file at `.cloudzilla/workflows/*.yml` or `*.yaml` in a Repository, in a documented subset of the
GitHub Actions syntax ([ADR 0004](./adr/0004-workflow-format-is-a-github-actions-subset.md)). The parser is
`internal/ci/workflow`: `Parse(path, data)` returns a validated `Workflow` with matrix jobs already expanded, or
line-numbered `Error`s. It is pure and knows nothing about the database.

## From a push to a result

A **Run** is one execution of one Workflow for one commit, numbered per Repository. It is created by:

- a push to a branch or tag whose `on.push` filters match (a push that only deletes refs creates none), including the
  new tip a pull request merge leaves on its base branch;
- a pull request that is opened, reopened or has new commits (`opened`, `reopened`, `synchronize`), run for the head
  commit;
- **Run workflow** on the Actions tab, or `POST /api/repos/{owner}/{repo}/actions/dispatch`, for a Workflow with
  `workflow_dispatch`.

The Workflow file is read from the pushed commit and stored with the Run, so a re-run at the same commit uses the same
file even if the branch has moved on. A file that does not parse creates a Run that failed at once, with zero Jobs, the
parse message on its page, and one failing commit status named `ci/<file name without extension>`, so a broken Workflow
cannot be merged around.

A Run is `waiting` (a fork pull request awaiting approval), `queued`, `running`, `success`, `failure` or `cancelled`; a
Job also has `skipped`. Each Job posts a commit status `ci/<job id>` (`ci/<job id> (<matrix values>)` for a matrix)
linked to its Run, so a required check in branch protection accepts built-in and external CI alike. A skipped Job
reports `success`, a cancelled one `error`, so a required check never passes by being cancelled. Two Workflows that use
the same Job id on one commit share a `ci/<job id>` status.

**Where results show.** The Actions tab lists Runs, with filters for workflow, branch, status and trigger; a Run page
shows the Job graph and the Steps, streams a running Job's log every 2 seconds, and has line anchors (`#L<n>`), a
timestamps toggle, log search and downloads for the log and the artifacts. A writer can cancel a Run, re-run all of it
or only its failed Jobs, run a Workflow by hand, and approve a waiting fork Run. The pull request **Checks** tab groups
each Run's Jobs under it, with **View run**, and keeps statuses that external CI posted in an **Other statuses** card.
The commit list, the pull request header and the file tree's last-commit row show the combined state of the commit as an
icon that links to its latest Run (or to the commit when only external CI reported). `/{owner}/{repo}/checks` still
resolves: it redirects to the Actions tab, where **Commit statuses** lists what external CI posted.

**Switches.** `ci.enabled` (default on) hides the tab, the routes and the schedulers when off, and creates no Runs.
Settings, CI/CD has a per-repository toggle that stops new Runs and keeps history. An archived repository creates no
Runs, and re-running or approving one of its Runs is refused too (`ci_off`), but its logs and artifacts stay readable. A
private repository, and a soft-deleted one, answers a viewer who cannot read it with the same 404 as a repository that
does not exist.

**Who may do what.**

| Action                                                           | Needs       |
| ---------------------------------------------------------------- | ----------- |
| View Runs, logs and artifacts                                    | `CanRead`   |
| Run workflow, cancel, re-run, approve a fork Run                 | `CanWrite`  |
| Repository secrets, repository runners, the CI/CD toggle         | `CanManage` |
| Instance runners and the read-only CI limits at `/admin/runners` | superadmin  |

Scheduling limits (`ci.*`) are in [configuration](./configuration.md) (the `ci.*` rows). A new Run past
`ci.max_queued_runs_per_repo` cancels the repository's oldest `queued` Run. A new push cancels the older queued Runs of
the same Workflow and ref, and `concurrency` with `cancel-in-progress` also stops running ones in the same group. A Job
queued for more than 24 hours is cancelled as "no runner".

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

Logs and artifacts have their own limits, listed under [Logs and artifacts](#logs-and-artifacts).

## Not supported

Each of these fails the parse with the file, line and key, as does any key not listed above:

`uses` (actions and reusable workflows), `services`, `schedule`, `paths` filters, `pull_request_target`, `permissions`,
`outputs`, `environment`, `defaults`, `workflow_call`, `workflow_dispatch` inputs.

`actions/upload-artifact` is a `uses:` step, so Cloudzilla has the job-level `artifacts:` list instead. An artifact
`name` can use `${{ matrix.* }}`; the runner interpolates it before the upload.

Job names must be unique in a Run: two Jobs with the same `name:`, a Job id equal to another Job's `name:`, and
duplicate matrix values or `include` entries fail the parse with the line of the Job.

## Runners

Workflow code runs only in a container started by a `cz-runner` process
([ADR 0003](./adr/0003-workflow-code-runs-only-on-runners-in-containers.md)). The server has no Docker access and never
connects to a Runner: the runner polls over outbound HTTPS, so a runner behind NAT needs no inbound port.

### Registering a runner

A superadmin creates an instance runner at `/admin/runners`; a repository admin creates one for a single repository
under Settings, Runners. **New runner** shows a one-time registration token (`czreg_…`, valid 7 days, stored hashed) and
the command to run on the runner host:

```sh
cz-runner register --server https://git.example.com --token czreg_… --labels linux,docker \
  --image linux=ubuntu:24.04 --allowed-image 'golang:*'
cz-runner run
```

`register` trades the token for a long-lived runner token (`czrun_…`, stored hashed on the server) and writes
`<user config dir>/cz-runner/config.yaml` with mode 0600 (`--config` picks another path). Revoking a runner takes effect
on its next request. A Job it is running then fails as "runner lost".

| Config key       | Meaning                                                                                                            |
| ---------------- | ------------------------------------------------------------------------------------------------------------------ |
| `server`         | Cloudzilla base URL                                                                                                |
| `labels`         | Matched against a Job's `runs-on`: a runner takes a Job only if it holds every label the Job lists                 |
| `images`         | Label to the image a Job with that label runs in when its workflow names no `container`                            |
| `allowed_images` | Images a workflow's `container:` may name; a trailing `*` matches a prefix. Empty allows any. `images` always pass |

A refused image fails the Job with `image "…" is not in this runner's allowed_images`. A repository runner only claims
that repository's Jobs; an instance runner claims any. A Job whose labels match no runner stays `queued` (and is
cancelled as "no runner" after 24 hours).

`cz-runner run` flags: `--cpus` (default 2), `--memory` (4g, also the swap limit), `--pids-limit` (512), `--user`
(1000:1000, never root), `--docker` (the CLI to run), `--once`. A runner takes one Job at a time; run several for
parallelism. Build it with `make build-runner` (`dist/cz-runner`, a static binary).

### What runs, and how

For each Job the runner creates a network and a volume named `cz-job-<id>`, clones the Job's commit with go-git into a
host temp directory using the per-job token, creates one container named `cz-job-<id>`, copies the checkout into its
volume and starts it idling (`sleep`). Each Step then runs as a `docker exec` into that container, so tools a Step
installs are still there in the next. The container, volume and network are removed when the Job ends, and when the
runner stops on `SIGINT` or `SIGTERM`. A Step sees the checkout at `/workspace/src` (`GITHUB_WORKSPACE`, the default
working directory) and `HOME=/workspace/home`. A Step's environment and script never appear in `docker exec` arguments,
where any local user could read them with `ps`: the runner writes each to a `0600` file under `/workspace/home/.cz-step`
over stdin, and the Step sources and deletes them.

The Job's container gets exactly these flags and nothing a workflow says can change them:

```
--privileged=false --cap-drop=ALL --security-opt=no-new-privileges --user=1000:1000
--cpus=2 --memory=4g --memory-swap=4g --pids-limit=512 --network=cz-job-<id> --volume=cz-job-<id>:/workspace
```

`docker exec` inherits the user, limits, network and volume and is given only `--workdir` and `--env`. No host path is
mounted and the Docker socket is never passed in. The image, `env` names, `working-directory` and `shell` are validated
before anything starts (an image beginning with `-` or containing spaces, a working directory outside the workspace, or
a shell other than `sh` or `bash` fails the Step). Step `if`, `env`, `run` and `${{ }}` are evaluated on the runner. A
Step's `timeout-minutes` kills the processes it started (the Job continues, so `if: always()` Steps still run); the Job
timeout removes the container.

### Operating a runner

- **Several runners.** A runner takes one Job at a time; start more processes, with the same or different labels, for
  parallelism. Give a Job's `runs-on` the labels only the right runners hold (`gpu`, `arm64`) to steer it.
- **Revoking.** Revoke a runner from the same page that registered it; its next request is refused and any Job it holds
  fails as "runner lost". Revoke a runner you no longer trust before anything else.
- **Upgrading.** Stop the runner (`SIGTERM` removes its containers), replace the binary, start it. Its Job is lost and
  can be re-run.
- **Disk.** Each Job's checkout and volume are removed when it ends; `docker system prune` on the host clears images
  nobody uses.
- **Repo runners.** A repository runner claims only that repository's Jobs, so a repository with sensitive builds can
  have a host of its own.

### Runner protocol

All calls are `POST`, JSON, with `Authorization: Bearer <token>`. Types are in `internal/ci/runnerapi`.

| Endpoint                                 | Token        | Effect                                                                         |
| ---------------------------------------- | ------------ | ------------------------------------------------------------------------------ |
| `/api/runner/register`                   | registration | One-time: returns the runner token                                             |
| `/api/runner/claim?wait=<s>`             | runner       | Long poll (at most 30 s). `200` with a `JobSpec` and a per-job token, or `204` |
| `/api/runner/jobs/{id}/heartbeat`        | runner       | Renews the 60 s lease; `continue: false` means stop (cancelled)                |
| `/api/runner/jobs/{id}/steps/{position}` | runner       | `running`, `success`, `failure` or `skipped`                                   |
| `/api/runner/jobs/{id}/finish`           | runner       | `success`, `failure` or `cancelled`; `409` once the Job has ended              |
| `/api/runner/jobs/{id}/log`              | per-job      | Text body of whole lines; `?seq=<n>&step=<position>`. See below                |
| `/api/runner/jobs/{id}/artifacts`        | per-job      | Zip body; `?name=<name>`. See below                                            |

A runner may touch only a Job it holds (`403` otherwise). A Job with no heartbeat for 60 s fails as "runner lost" and
its Run fails; the sweep runs every 15 s. The runner sends a heartbeat every 10 s.

The per-job token (`czj_…`) is minted at claim and stored hashed on the Job. It is read-only, bound to that Job's
repository, valid for `git-upload-pack` over HTTP Basic and nothing else (a push is `403`, another repository `404`, any
user API `401`), and stops working the moment the Job leaves `running`. The runner never passes it into a Step.

The log and artifact endpoints take this token for their own Job only (`403` for another Job, `401` once the Job has
left `running`), so the runner posts the artifacts before it reports the Job finished. They are not JSON: the log body
is text and the artifact body is a zip.

## Security model

Workflow code is whatever its author pushed, and a pull request proposes more. It runs only on a Runner, in a
non-privileged container, never in the server process and never with the Instance's credentials
([ADR 0003](./adr/0003-workflow-code-runs-only-on-runners-in-containers.md)). The container flags are listed under
[What runs, and how](#what-runs-and-how). Egress is not filtered by Cloudzilla; see
[Operator hardening](#operator-hardening).

### Secrets

A repository admin (`CanManage`) stores secrets under Settings, CI/CD. A name is letters, digits and underscores, not
starting with a digit, up to 100 characters; a value is 1 byte to 16 KiB; a repository holds 100. Values are sealed with
`security.secret_key` (the same box and per-purpose key derivation as mirror tokens), are write-only in the UI, and
saving one is refused while the key is not set.

A Job reads a secret as `${{ secrets.NAME }}`, resolved on the runner: the server sends the values with the claim, only
for the Job's repository and never for a fork Run. The runner and then the server mask a value in the log (the exact
text, its base64 form and its URL-encoded form). A secret that contains a newline is also masked line by line (each line, with its base64 and URL-encoded forms), so a
log chunk that ends mid-secret cannot reveal a line; lines shorter than 4 characters are not masked on their own, as
they would redact ordinary output.

**A writer can read a secret.** Anyone who can push a branch or open a pull request from a branch of the same repository
runs a workflow of their own with the repository's secrets, so they can print one in a form the masker does not know
(reversed, split, hex-encoded, sent over the network). Treat a secret as readable by every writer, give each secret the
least privilege it needs, and keep production credentials out of repositories whose writers you do not trust.

Do not echo a secret or paste it into a command line, where it is also visible to every process in the container. Pass
it to the one Step that needs it through `env`:

```yaml
steps:
  # Wrong: the value is inlined into the script and into the log line that echoes it.
  - run: curl -H "Authorization: Bearer ${{ secrets.DEPLOY_TOKEN }}" https://deploy.example.com
  # Right: the Step sees an environment variable and the script never contains the value.
  - run: curl -H "Authorization: Bearer $DEPLOY_TOKEN" https://deploy.example.com
    env:
      DEPLOY_TOKEN: ${{ secrets.DEPLOY_TOKEN }}
```

### Fork pull requests

A pull request whose head repository differs from its base creates a `waiting` Run: its Workflow file is stored but
nothing runs. A writer approves it from the Run page or the pull request Checks tab (**Approve**), after reading the
Workflow file in the pull request, because approving runs that code on your runners. Every new push to the pull request
makes a new Run that waits again, so approving version 1 never covers version 2. An approved fork Run gets no secrets
and a read-only per-job token. A reader never sees **Approve**.

### The per-job token

Each claimed Job gets a token that is read-only, bound to one repository and one Job, valid for a git clone over HTTP
and for posting that Job's logs and artifacts, and dead the moment the Job leaves `running`. It is not passed into
Steps. Details are under [Runner protocol](#runner-protocol).

A waiting claim looks for a Job after 1 s, then 2 s, 4 s and every 5 s, so an idle runner is cheap and a new Job starts
within 5 s. The route has no write deadline, so the 25 s wait never hits the server's `write_timeout`. The server writes
a runner's `last_contact_at` at most every 5 s. A claim builds the Job's spec in the same transaction, so a failure
leaves the Job `queued` for the next poll.

## Logs and artifacts

**Logs.** While a Step runs the runner posts its output every second, or sooner at 256 KiB, as chunks of whole lines.
Each chunk carries a sequence number `seq` chosen by the runner, so a retried POST is stored once, and the Step it
belongs to. The server assigns the line offsets, stores the chunk as an object and then its row, and records the Step's
line range. A chunk may be up to 1 MiB. The runner masks secret values (exact, base64 and URL-encoded forms) before it
posts, and the server masks the repo's secret values again as a backstop, so a runner that skips it still can't store
one. A secret that spans two lines is not masked across the break.

A Job's log is capped at `ci.max_log_bytes` (20 MiB). The chunk that crosses it is cut at a line boundary and ends with
one line, `[log truncated: this Job reached the 20 MiB limit (ci.max_log_bytes)]`; later chunks are dropped and the
reply tells the runner to stop sending.

**Reading a log.**

| Endpoint                                                             | Returns                                                                                     |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `GET /api/repos/{owner}/{repo}/actions/runs/{number}/jobs/{id}/logs` | JSON `{offset, next_offset, total_lines, truncated, complete, lines}`; `?offset=` `?limit=` |
| `GET /{owner}/{repo}/actions/runs/{number}/jobs/{id}/log`            | The whole log as a `text/plain` download                                                    |
| `GET /api/repos/{owner}/{repo}/actions/runs/{number}/artifacts`      | JSON list with `download_url`                                                               |
| `GET /{owner}/{repo}/actions/runs/{number}/artifacts/{artifact id}`  | The artifact's zip as a download                                                            |

`limit` defaults to 1000 lines and is at most 5000. A reader polls with `offset=<next_offset>` until `complete` is true
and a page comes back empty. Everything needs `CanRead` on every request. A viewer who can't read the repo gets the same
`404` as for a repo that doesn't exist (`repo not found` from the JSON API, an empty-bodied `404` on the downloads).

**Artifacts.** A Job's `artifacts:` list names paths in the checkout. After the Steps, whether or not the Job passed,
the runner copies each path out of the Job's container, rewrites it as a zip (directories and regular files only: a
symlink would point out of the archive when unpacked) and uploads it. A path that does not exist, or an upload the
server refuses, fails the Job with a message naming the artifact. The archive is downloaded as `<name>.zip`.

## Limits and retention

| Key                        | Default | Meaning                                                           |
| -------------------------- | ------- | ----------------------------------------------------------------- |
| `ci.max_log_bytes`         | 20 MiB  | Log per Job; the rest is dropped after a truncation line          |
| `ci.max_artifact_bytes`    | 100 MiB | One artifact's zip; larger is refused with `413` naming the limit |
| `ci.max_artifacts_per_run` | 10      | Artifacts per Run attempt; the next is refused with `422`         |
| `ci.log_retention`         | `720h`  | Age after which a log chunk is deleted                            |
| `ci.artifact_retention`    | `720h`  | Age after which an artifact is deleted; it stops downloading then |

An hourly worker deletes expired chunks and artifacts. The Run, its Jobs and Steps, and commit statuses stay; a Job
whose chunks have expired reads as an empty log. Storage layout, deletion order and orphans are in
[Storage](./storage.md#ci-logs-and-artifacts).

### Operator hardening

The runner limits what a container can do on the host, not where it can connect. Block egress from the runner host's
Docker networks to what a Job must not reach:

- the cloud metadata address `169.254.169.254` and the rest of `169.254.0.0/16`;
- private ranges you do not want Jobs to touch (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), except what builds
  need;
- the Cloudzilla server itself on any port but the HTTPS one the runner uses, and its database.

Run the runner as an unprivileged user in the `docker` group on a dedicated host: access to the Docker socket is root on
that host, so do not share it with anything else. Keep the runner's config file private (it holds the runner token).
After a crash or `kill -9`, containers named `cz-job-*` may remain; remove them with `docker rm -f` and the matching
volumes and networks with `docker volume rm` and `docker network rm`.

`docker compose --profile runner up` starts a DEV ONLY runner against the host Docker socket (`Dockerfile.cz-runner`);
set `CZ_RUNNER_TOKEN` to a registration token from `/admin/runners` and `CZ_SERVER_BASE_URL=http://cloudzilla:8080` on
the server so its clone URL resolves from the runner's container.

## Webhook and API

A `workflow_run` webhook fires once when a Run reaches a final status (success, failure or cancelled), after the status
is committed, to hooks that list `workflow_run`. Existing hooks do not get it until someone adds it. The payload and
signing are in [webhooks](./webhooks.md#workflow_run); the endpoints (list, get, logs, cancel, re-run, dispatch) are in
the [API reference](./api-reference.md#workflow-runs), and the `repo` and `repo:write` scopes they need are in
[access control](./access-control.md#token-scopes). The `cz` CLI has no CI commands in v1.

## Troubleshooting

| Symptom                                            | Cause and fix                                                                                                                                                                                   |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A push made no Run                                 | The file is not under `.cloudzilla/workflows/` with a `.yml` or `.yaml` extension, its `on` does not match the ref, CI is off for the repository or the Instance, or the repository is archived |
| A Run failed at once with no Jobs                  | The Workflow does not parse: the Run page shows the file, line and key. A key under [Not supported](#not-supported) is the usual cause                                                          |
| Jobs stay `queued`                                 | No online runner holds every label in `runs-on`; the Run page names the missing label. Register one, or fix the label                                                                           |
| "No runner" after a day                            | The Job was queued 24 hours with no matching runner and was cancelled; re-run it once a runner exists                                                                                           |
| Job failed as "runner lost"                        | No heartbeat for 60 s: the runner stopped, lost its network or was revoked. Start it and re-run the failed Jobs                                                                                 |
| `image "…" is not in this runner's allowed_images` | The Workflow's `container:` is outside the runner's `allowed_images`; add it to the runner config or use an allowed image                                                                       |
| A Step cannot reach the network or a host          | Egress is the operator's firewall, not Cloudzilla's. Check the runner host's rules                                                                                                              |
| `secrets.NAME` is empty                            | The secret is not saved for this repository, or the Run is a fork Run, which never gets secrets                                                                                                 |
| Saving a secret is refused                         | `security.secret_key` is not set on the Instance                                                                                                                                                |
| A pull request Run says "Approval required"        | It is a fork Run; a writer approves it from the Run page or the Checks tab                                                                                                                      |
| A log ends with `[log truncated: …]`               | The Job hit `ci.max_log_bytes`; raise it or print less                                                                                                                                          |
| An artifact will not download                      | It passed `ci.artifact_retention`, or the Job's path did not exist and the Job failed                                                                                                           |

## Known limitations

- **Fork pull requests cannot exist yet.** The app has no cross-repository pull requests, so the fork approval flow is
  covered by tests only and has never run on a real fork. When they land, `WorkflowRunService.OnPullRequest` must be
  called with the fork as the head repository, and a fork Run's per-job token must be bound to the fork so the runner
  can clone its commit.
- **A `kill -9` of a runner leaves `cz-job-*` containers, volumes and networks behind.** `SIGINT` and `SIGTERM` clean
  up; remove the rest by hand (see [Operator hardening](#operator-hardening)).
- **`strategy.fail-fast` is parsed but not acted on.** Matrix siblings keep running after one fails.
- **`/admin/runners` is reachable, and its tab shown, when `ci.enabled` is off.** The page says CI is off; the runner
  API is absent.
- **A matrix expression in an `artifacts:` name is not interpolated** (`dist-${{ matrix.arch }}` is kept as written).
- **One Job container per Job.** Steps share it, so a tool one Step installs is there in the next, but a Job cannot
  start service containers (`services` is rejected).
