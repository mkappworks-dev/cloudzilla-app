# Prometheus metrics on a separate listener

Created: 2026-10-06
Category: enhancement
Status: needs-triage
Spec: [../spec.md](../spec.md) (Metrics)

The open questions 1 (exposition) and 2 (protection) in the spec decide the shape of this issue. It's written for the recommended answers: hand-written exposition, and a separate listen address.

## What to build

**Metrics package.** `internal/metrics` is stdlib only.

- **Types:** counters, gauges and histograms with fixed label names, plus scrape-time gauge callbacks.
- **Output:** `WriteText(io.Writer)` writes Prometheus text format 0.0.4. Families are sorted, with `# HELP` and `# TYPE` lines. Label values escape `\` `"` and newline. Histograms write cumulative `_bucket{le="…"}` lines including `+Inf`, plus `_sum` and `_count`.
- **Names:** every metric is declared in one file.

**Config.** A `metrics.listen_addr` key (`CZ_METRICS_LISTEN_ADDR`, default empty, meaning off) in `internal/config`. Document it in `docs/configuration.md`.

**Listener.** When the address is set, `cmd/server/main.go` starts a second `http.Server` that serves only `GET /metrics`, and shuts it down with the others. The main router never serves `/metrics`.

**Instrumentation**, using the spec's starting set:

- **HTTP:** a middleware placed early in the global chain records requests total, duration and in-flight, labelled by method, chi route pattern (`unmatched` when there is none) and status code. The health paths from issue 01 are never counted.
- **Git:** operations and bytes on both transports. Receive-pack uses the existing `ByteCounter`. Upload-pack needs a counting writer around `resp.Encode`.
- **Imports:** a jobs gauge by state, read from `ImportService` at scrape time, and an imports-total counter by result.
- **Webhooks:** deliveries total by attempt and result. A `webhook_retries_due` gauge is set on each retry tick.
- **DB pool:** gauges and counters from `sql.DB.Stats()` at scrape time.
- **Process:** `build_info`, `go_goroutines` and `process_start_time_seconds`.

**Docs.** A "Metrics" section in `docs/deployment.md` with a Prometheus `scrape_config` example, the full metric list, and a warning not to publish or proxy the port. Mention that compose doesn't publish it by default.

## Acceptance criteria

- [ ] With `metrics.listen_addr` empty, no extra port is opened.
- [ ] With it set, `GET /metrics` on that address returns 200 with `Content-Type: text/plain; version=0.0.4; charset=utf-8`.
- [ ] `GET /metrics` on the main port returns the app's 404 page.
- [ ] Output parses as Prometheus text format: every family has HELP and TYPE, histogram buckets are cumulative and end in `+Inf`, and `_count` equals the `+Inf` bucket.
- [ ] A request to `/{owner}/{repo}/issues/{number}` is counted under that route pattern, not under the concrete path.
- [ ] An unknown path is counted as `route="unmatched"`.
- [ ] `/healthz` and `/readyz` aren't counted.
- [ ] A push and a clone over HTTP, and the same over SSH, each increment `git_operations_total` and `git_bytes_total` with the right `transport` and `service`.
- [ ] A finished import increments `imports_total` with its result.
- [ ] A webhook delivery increments `webhook_deliveries_total` with its attempt and result.
- [ ] `db_connections` reflects `sql.DB.Stats()` at scrape time.
- [ ] No new module in `go.mod`.

## Tests

- **Golden-output unit tests** for `WriteText`: escaping, sorting, histogram rendering, and empty families.
- **Router-level test:** route-pattern labels, `unmatched`, and health paths excluded.
- **Git transport:** extend the existing handler and SSH tests to assert counter deltas.
- **Services:** unit tests for import and webhook counters, using the existing service test seams.
- **Listener:** a test that starts it on `127.0.0.1:0` and scrapes it.

## Files

- `internal/metrics/` (new)
- `internal/config/config.go`
- `cmd/server/main.go`
- `internal/router/router.go`
- `internal/middleware/` (new metrics middleware)
- `internal/handler/git_http.go`
- `internal/ssh/server.go`
- `internal/gittransport/observe.go`
- `internal/service/import_service.go`
- `internal/service/webhook_service.go`
- `docs/configuration.md`
- `docs/deployment.md`
