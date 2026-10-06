package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/metrics"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
)

func metricsRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(Metrics)
	r.Get("/{owner}/{repo}/issues/{number}", func(w http.ResponseWriter, _ *http.Request) {})
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	return r
}

func count(method, route, code string) float64 {
	return promtestutil.ToFloat64(metrics.HTTPRequests.WithLabelValues(method, route, code))
}

func TestMetrics_CountsUnderRoutePattern(t *testing.T) {
	const route = "/{owner}/{repo}/issues/{number}"
	before := count("GET", route, "200")
	metricsRouter().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/alice/app/issues/7", nil))
	if got := count("GET", route, "200") - before; got != 1 {
		t.Fatalf("pattern counter delta = %v, want 1", got)
	}
}

func TestMetrics_UnmatchedAndOtherMethod(t *testing.T) {
	before := count("other", "unmatched", "405")
	metricsRouter().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("BREW", "/nope/at/all/x/y", nil))
	if got := count("other", "unmatched", "405") - before; got != 1 {
		t.Fatalf("unmatched counter delta = %v, want 1", got)
	}
}

func TestMetrics_PanicCountedAs500(t *testing.T) {
	before := count("GET", "/boom", "500")
	func() {
		defer func() { _ = recover() }()
		metricsRouter().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	}()
	if got := count("GET", "/boom", "500") - before; got != 1 {
		t.Fatalf("panic counter delta = %v, want 1", got)
	}
	if got := promtestutil.ToFloat64(metrics.HTTPInFlight); got != 0 {
		t.Fatalf("in-flight = %v after panic, want 0", got)
	}
}
