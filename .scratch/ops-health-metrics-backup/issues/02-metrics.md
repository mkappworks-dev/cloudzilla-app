# Prometheus metrics on a separate listener

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Spec: [../spec.md](../spec.md) (Metrics)

Shaped by the spec's decisions 1 (`client_golang`) and 2 (a separate listen address).

## What to build

**Dependency.** Add `github.com/prometheus/client_golang` (`prometheus`, `prometheus/collectors`, `prometheus/promhttp`).

**Metrics package.** `internal/metrics` declares every Cloudzilla metric in one file, registered on its own `prometheus.Registry` rather than the global default. It also registers:

- `collectors.NewGoCollector()`;
- `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})`;
- `collectors.NewDBStatsCollector(db, "cloudzilla")`.

`metrics.Handler()` returns `promhttp.HandlerFor(registry, promhttp.HandlerOpts{})`.

**Config.** A `metrics.listen_addr` key (`CZ_METRICS_LISTEN_ADDR`, default empty, meaning off) in `internal/config`. Document it in `docs/configuration.md`.

**Listener.** When the address is set, `cmd/server/main.go` starts a second `http.Server` that serves only `GET /metrics`, and shuts it down with the others. The main router never serves metrics. There, `/metrics` stays an ordinary `/{owner}` path.

**Instrumentation**, using the spec's starting set:

- **HTTP:** a middleware placed early in the global chain records requests total, duration and in-flight, labelled by method, chi route pattern (`unmatched` when there is none) and status code. The health paths from issue 01 never reach it, because they're answered before the router.
- **Git:** operations and bytes on both transports. Receive-pack uses the existing `ByteCounter`. Upload-pack needs a counting writer around `resp.Encode`.
- **Imports:** a jobs gauge by state, read from `ImportService` at scrape time through a `GaugeFunc` or collector, and an imports-total counter by result.
- **Webhooks:** deliveries total by attempt and result. A `webhook_retries_due` gauge is set on each retry tick.
- **Build:** `cloudzilla_build_info{version}`.

**Docs.** A "Metrics" section in `docs/deployment.md`:

- a Prometheus `scrape_config` example;
- the full list of Cloudzilla metrics;
- a warning not to publish or proxy the port;
- the compose snippet that sets `CZ_METRICS_LISTEN_ADDR=:9090` without publishing it.

## Acceptance criteria

- [ ] With `metrics.listen_addr` empty, no extra port is opened.
- [ ] With it set, `GET /metrics` on that address returns 200 in Prometheus text format, including `go_*`, `go_sql_*{db_name="cloudzilla"}` and `cloudzilla_build_info`.
- [ ] The main port doesn't serve metrics: `GET /metrics` there is handled by the `/{owner}` route.
- [ ] A request to `/{owner}/{repo}/issues/{number}` is counted under that route pattern, not under the concrete path.
- [ ] An unknown path is counted as `route="unmatched"`.
- [ ] `/healthz` and `/readyz` aren't counted.
- [ ] A push and a clone over HTTP, and the same over SSH, each increment `cloudzilla_git_operations_total` and `cloudzilla_git_bytes_total` with the right `transport` and `service`.
- [ ] A finished import increments `cloudzilla_imports_total` with its result.
- [ ] A webhook delivery increments `cloudzilla_webhook_deliveries_total` with its attempt and result.
- [ ] `go.mod` gains only `client_golang` and the modules it requires.

## Tests

- **Library helpers.** Use `client_golang`'s `prometheus/testutil` (`ToFloat64`, `GatherAndCompare`). Alias the import, because `internal/testutil` exists.
- **Router-level test:** route-pattern labels, `unmatched`, and health paths excluded.
- **Git transport:** extend the existing handler and SSH tests to assert counter deltas.
- **Services:** unit tests for import and webhook counters, using the existing service test seams.
- **Listener:** a test that starts it on `127.0.0.1:0` and scrapes it.

## Files

- `go.mod`, `go.sum`
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
