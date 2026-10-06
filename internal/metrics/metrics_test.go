package metrics

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListener_ServesOnlyMetrics(t *testing.T) {
	SetBuildInfo("test")
	if err := RegisterImportJobs(func() (int, int) { return 1, 2 }); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: Mux(), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Get("http://" + ln.Addr().String() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, want := range []string{"go_goroutines", `cloudzilla_build_info{version="test"} 1`, `cloudzilla_import_jobs{state="running"} 2`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("scrape lacks %q", want)
		}
	}

	other, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Body.Close()
	if other.StatusCode != http.StatusNotFound {
		t.Errorf("GET / status = %d, want 404", other.StatusCode)
	}
}
