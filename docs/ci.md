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

Logs and artifacts have their own limits, listed under [Logs and artifacts](#logs-and-artifacts).

## Not supported

Each of these fails the parse with the file, line and key, as does any key not listed above:

`uses` (actions and reusable workflows), `services`, `schedule`, `paths` filters, `pull_request_target`, `permissions`,
`outputs`, `environment`, `defaults`, `workflow_call`, `workflow_dispatch` inputs.

`actions/upload-artifact` is a `uses:` step, so Cloudzilla has the job-level `artifacts:` list instead.

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
working directory) and `HOME=/workspace/home`.

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

## Logs and artifacts

**Logs.** While a Step runs the runner posts its output every second, or sooner at 256 KiB, as chunks of whole lines.
Each chunk carries a sequence number `seq` chosen by the runner, so a retried POST is stored once, and the Step it
belongs to. The server assigns the line offsets, stores the chunk as an object and then its row, and records the Step's
line range. A chunk may be up to 1 MiB. The runner masks secret values (exact, base64 and URL-encoded forms) before it
posts, and the server masks the repo's secret values again as a backstop, so a runner that skips it still can't store
one. A secret that spans two lines is not masked across the break. No Job carries secrets yet; the hook is
`WorkflowLogService.WithSecrets`.

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
