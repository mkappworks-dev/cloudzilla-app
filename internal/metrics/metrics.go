// Package metrics declares every Cloudzilla Prometheus metric on a private registry.
package metrics

import (
	"database/sql"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Not the global default registry, so no library can add series behind our back.
var Registry = prometheus.NewRegistry()

var factory = promauto.With(Registry)

var (
	HTTPRequests = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudzilla_http_requests_total",
		Help: "HTTP requests served, by method, chi route pattern and status code.",
	}, []string{"method", "route", "code"})

	HTTPDuration = factory.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cloudzilla_http_request_duration_seconds",
		Help:    "HTTP request duration, by method and chi route pattern.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"method", "route"})

	HTTPInFlight = factory.NewGauge(prometheus.GaugeOpts{
		Name: "cloudzilla_http_requests_in_flight",
		Help: "HTTP requests currently being served.",
	})

	GitOperations = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudzilla_git_operations_total",
		Help: "Git transport exchanges. One HTTP fetch can take several upload-pack exchanges, so this isn't a count of user-level fetches.",
	}, []string{"transport", "service", "result"})

	GitBytes = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudzilla_git_bytes_total",
		Help: "Bytes received (receive-pack) or sent (upload-pack) over git transports.",
	}, []string{"transport", "service"})

	Imports = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudzilla_imports_total",
		Help: "Finished repository imports, by result.",
	}, []string{"result"})

	WebhookDeliveries = factory.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudzilla_webhook_deliveries_total",
		Help: "Webhook delivery attempts, by attempt kind and result.",
	}, []string{"attempt", "result"})

	WebhookRetriesDue = factory.NewGauge(prometheus.GaugeOpts{
		Name: "cloudzilla_webhook_retries_due",
		Help: "Webhook deliveries due for retry at the last retry tick.",
	})

	buildInfo = factory.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cloudzilla_build_info",
		Help: "Build information; the value is always 1.",
	}, []string{"version"})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

func SetBuildInfo(version string) { buildInfo.WithLabelValues(version).Set(1) }

// RegisterDB exports the pool's stats as go_sql_*{db_name="cloudzilla"}.
func RegisterDB(db *sql.DB) error {
	return Registry.Register(collectors.NewDBStatsCollector(db, "cloudzilla"))
}

// RegisterImportJobs reads the gauge at scrape time, because the jobs live in ImportService's memory.
func RegisterImportJobs(counts func() (queued, running int)) error {
	return Registry.Register(&importJobsCollector{counts: counts})
}

type importJobsCollector struct{ counts func() (queued, running int) }

var importJobsDesc = prometheus.NewDesc("cloudzilla_import_jobs", "Repository imports in memory, by state.", []string{"state"}, nil)

func (c *importJobsCollector) Describe(ch chan<- *prometheus.Desc) { ch <- importJobsDesc }

func (c *importJobsCollector) Collect(ch chan<- prometheus.Metric) {
	queued, running := c.counts()
	ch <- prometheus.MustNewConstMetric(importJobsDesc, prometheus.GaugeValue, float64(queued), "queued")
	ch <- prometheus.MustNewConstMetric(importJobsDesc, prometheus.GaugeValue, float64(running), "running")
}

func Handler() http.Handler { return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}) }

// Mux serves only GET /metrics.
func Mux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", Handler())
	return mux
}

// GitOp records one transport exchange.
func GitOp(transport, service string, ok bool, bytes int64) {
	result := "ok"
	if !ok {
		result = "error"
	}
	GitOperations.WithLabelValues(transport, service, result).Inc()
	if bytes > 0 {
		GitBytes.WithLabelValues(transport, service).Add(float64(bytes))
	}
}
