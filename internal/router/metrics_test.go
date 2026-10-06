package router_test

import (
	"net/http"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/metrics"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_HealthProbesAreNotCounted(t *testing.T) {
	h := newProbeRouter(t, probeStub{})
	total := func() int { return promtestutil.CollectAndCount(metrics.HTTPRequests) }
	before := total()

	for _, path := range []string{"/healthz", "/readyz"} {
		if rr := request(h, http.MethodGet, path); rr.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rr.Code)
		}
	}

	if got := total(); got != before {
		t.Errorf("http_requests_total series went %d -> %d; probes must not be counted", before, got)
	}
}
