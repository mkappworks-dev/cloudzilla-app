package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func webhookTestServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestWebhookSend_RefusesLoopback(t *testing.T) {
	srv, hits := webhookTestServer(t, ok)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	s := &WebhookService{client: newWebhookHTTPClient(false)}

	for _, host := range []string{"127.0.0.1", "localhost"} {
		code, err := s.send(context.Background(), model.Webhook{URL: "http://" + host + ":" + port + "/hook"}, "push", []byte(`{}`))
		var blocked *PrivateNetworkError
		if !errors.As(err, &blocked) || blocked.Host != host {
			t.Errorf("%s: send = (%d, %v), want PrivateNetworkError{%s}", host, code, err, host)
		}
		if got := deliveryError(err); got != host+" resolves to a private network address" {
			t.Errorf("%s: deliveryError = %q", host, got)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("server was reached %d times", hits.Load())
	}
}

func TestWebhookSend_AllowLocalNetworks(t *testing.T) {
	var sig string
	srv, _ := webhookTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sig = r.Header.Get("X-Hub-Signature-256")
		w.WriteHeader(http.StatusNoContent)
	})
	s := &WebhookService{client: newWebhookHTTPClient(true)}
	code, err := s.send(context.Background(), model.Webhook{URL: srv.URL, Secret: "k"}, "push", []byte(`{}`))
	if err != nil || code != http.StatusNoContent {
		t.Fatalf("send = (%d, %v), want 204", code, err)
	}
	if sig != "sha256="+computeHMAC([]byte(`{}`), "k") {
		t.Errorf("signature = %q", sig)
	}
}

func TestWebhookSend_DoesNotFollowRedirects(t *testing.T) {
	target, targetHits := webhookTestServer(t, ok)
	srv, _ := webhookTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	})
	s := &WebhookService{client: newWebhookHTTPClient(true)}
	code, err := s.send(context.Background(), model.Webhook{URL: srv.URL}, "push", []byte(`{}`))
	if err != nil || code != http.StatusFound {
		t.Fatalf("send = (%d, %v), want 302", code, err)
	}
	if targetHits.Load() != 0 {
		t.Error("redirect target was reached")
	}
	if nextWebhookRetry(1, code, err) == nil {
		t.Error("a 3xx must be retried")
	}
}

func TestWebhookClient_IgnoresProxy(t *testing.T) {
	for _, allowLocal := range []bool{false, true} {
		if tr := newWebhookHTTPClient(allowLocal).Transport.(*http.Transport); tr.Proxy != nil {
			t.Errorf("allowLocal=%v: transport uses a proxy", allowLocal)
		}
	}
}

func TestNextWebhookRetry(t *testing.T) {
	blocked := &PrivateNetworkError{Host: "internal.example"}
	for _, tc := range []struct {
		name    string
		attempt int
		code    int
		err     error
		retry   bool
	}{
		{"success", 1, 200, nil, false},
		{"server error", 1, 500, nil, true},
		{"redirect", 2, 302, nil, true},
		{"network error", 3, 0, errors.New("connection refused"), true},
		{"private address", 1, 0, blocked, false},
		{"private address wrapped", 1, 0, &net.OpError{Op: "dial", Err: blocked}, false},
		{"attempt limit", webhookMaxAttempts, 500, nil, false},
	} {
		if got := nextWebhookRetry(tc.attempt, tc.code, tc.err) != nil; got != tc.retry {
			t.Errorf("%s: retry = %v, want %v", tc.name, got, tc.retry)
		}
	}
}

func TestCheckWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		url        string
		allowLocal bool
		wantReason string // "" means accepted
	}{
		{"http://8.8.8.8/hook", false, ""},
		{"https://[2606:4700:4700::1111]/hook", false, ""},
		{"https://does-not-resolve.invalid/hook", false, ""},
		{"ftp://8.8.8.8/hook", false, "http or https"},
		{"://bad", false, "http or https"},
		{"", false, "http or https"},
		{"http:///hook", false, "must include a host"},
		{"http://127.0.0.1/hook", false, "127.0.0.1 resolves to a private network address"},
		{"http://localhost:8080/hook", false, "localhost resolves to a private network address"},
		{"http://10.0.0.1/hook", false, "private network"},
		{"http://169.254.169.254/latest/meta-data/", false, "private network"},
		{"http://100.100.100.200/", false, "private network"},
		{"http://0.0.0.0/", false, "private network"},
		{"http://[::1]/hook", false, "private network"},
		{"http://127.0.0.1/hook", true, ""},
		{"ftp://127.0.0.1/hook", true, "http or https"},
	} {
		err := checkWebhookURL(context.Background(), tc.url, tc.allowLocal)
		if tc.wantReason == "" {
			if err != nil {
				t.Errorf("checkWebhookURL(%q, %v) = %v, want nil", tc.url, tc.allowLocal, err)
			}
			continue
		}
		var urlErr *WebhookURLError
		if !errors.As(err, &urlErr) || !strings.Contains(urlErr.Reason, tc.wantReason) {
			t.Errorf("checkWebhookURL(%q, %v) = %v, want WebhookURLError containing %q", tc.url, tc.allowLocal, err, tc.wantReason)
		}
	}
}

func TestDeliveryError_HidesTheResolver(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "hooks.example", Server: "10.0.0.2:53", IsNotFound: true}}
	if got := deliveryError(err); got != "lookup hooks.example: no such host" {
		t.Errorf("deliveryError = %q", got)
	}
}

func TestAddressDeadline_SharesTheBudget(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	if got := time.Until(addressDeadline(deadline, 2)); got > 5*time.Second || got < 4*time.Second {
		t.Errorf("first of two addresses gets %v, want about 5s", got)
	}
	if got := time.Until(addressDeadline(deadline, 1)); got < 9*time.Second {
		t.Errorf("last address gets %v, want the rest", got)
	}
	if got := time.Until(addressDeadline(time.Now().Add(3*time.Second), 10)); got < 1900*time.Millisecond {
		t.Errorf("share below the floor = %v, want about 2s", got)
	}
	if got := time.Until(addressDeadline(time.Now().Add(time.Second), 10)); got > time.Second {
		t.Errorf("share = %v, must not pass the deadline", got)
	}
}
