package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	events *[]string
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.sleeps = append(f.sleeps, d)
	if f.events != nil {
		*f.events = append(*f.events, "sleep "+d.String())
	}
	f.now = f.now.Add(d)
	return nil
}

func newDeviceClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{Host: srv.URL}
}

func TestRequestDeviceCodeSendsScopeAndNameWithoutAuth(t *testing.T) {
	c := newDeviceClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/auth/device/code" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("code request must not carry Authorization")
		}
		b, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(b))
		if v.Get("scope") != "repo:read issues:write" || v.Get("device_name") != "mk-laptop" {
			t.Errorf("form = %v", v)
		}
		_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri":"http://x/login/device","expires_in":900,"interval":5}`)
	})
	dc, err := c.RequestDeviceCode(context.Background(), []string{"repo:read", "issues:write"}, "mk-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if dc.DeviceCode != "dc" || dc.UserCode != "ABCD-EFGH" || dc.VerificationURI != "http://x/login/device" || dc.ExpiresIn != 900 || dc.Interval != 5 {
		t.Errorf("dc = %+v", dc)
	}
}

func TestRequestDeviceCodeErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
		notSupp bool
	}{
		{"404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, "--with-token", true},
		{"429 limit", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":"too_many_requests"}`)
		}, "retry in 1m0s", false},
		{"400 scope", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid_scope"}`)
		}, "invalid_scope", false},
		{"500", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":"server_error"}`)
		}, "server_error", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newDeviceClient(t, tt.handler).RequestDeviceCode(context.Background(), []string{"repo:write"}, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if got := errors.Is(err, ErrDeviceFlowUnsupported); got != tt.notSupp {
				t.Errorf("ErrDeviceFlowUnsupported = %v", got)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("not one line: %q", err)
			}
		})
	}
}

// pollServer answers each poll with the next canned reply and logs each poll into events.
func pollServer(t *testing.T, replies []string, events *[]string) *Client {
	t.Helper()
	i := 0
	return newDeviceClient(t, func(w http.ResponseWriter, r *http.Request) {
		*events = append(*events, "poll")
		if r.URL.Path != "/api/auth/device/token" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("poll must not carry Authorization")
		}
		b, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(b))
		if v.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || v.Get("device_code") != "dc" {
			t.Errorf("form = %v", v)
		}
		if i >= len(replies) {
			t.Errorf("unexpected poll %d", i+1)
			w.WriteHeader(500)
			return
		}
		status, body := 400, replies[i]
		if strings.Contains(body, "access_token") {
			status = 200
		}
		if body == "429" {
			status, body = 429, `{"error":"rate limit exceeded"}`
			w.Header().Set("Retry-After", "30")
		}
		i++
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

const (
	pending  = `{"error":"authorization_pending"}`
	slowDown = `{"error":"slow_down"}`
	granted  = `{"access_token":"czp_new","token_type":"bearer","scope":"repo:write"}`
)

func code(expires, interval int) *DeviceCode {
	return &DeviceCode{DeviceCode: "dc", ExpiresIn: expires, Interval: interval}
}

func TestPollDeviceTokenPendingThenSuccess(t *testing.T) {
	var events []string
	c := pollServer(t, []string{pending, pending, granted}, &events)
	clk := &fakeClock{now: time.Unix(0, 0), events: &events}
	tok, err := c.PollDeviceToken(context.Background(), code(900, 5), clk)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "czp_new" {
		t.Errorf("tok = %q", tok)
	}
	want := "sleep 5s,poll,sleep 5s,poll,sleep 5s,poll"
	if got := strings.Join(events, ","); got != want {
		t.Errorf("events = %s", got)
	}
}

func TestPollDeviceTokenSlowDownAddsFiveSecondsPermanently(t *testing.T) {
	var events []string
	c := pollServer(t, []string{slowDown, pending, slowDown, granted}, &events)
	clk := &fakeClock{now: time.Unix(0, 0), events: &events}
	if _, err := c.PollDeviceToken(context.Background(), code(900, 5), clk); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "sleep 5s,poll,sleep 10s,poll,sleep 10s,poll,sleep 15s,poll" {
		t.Errorf("events = %s", got)
	}
}

func TestPollDeviceTokenTerminalErrors(t *testing.T) {
	tests := []struct {
		reply string
		want  error
	}{
		{`{"error":"access_denied"}`, ErrDeviceDenied},
		{`{"error":"expired_token"}`, ErrDeviceExpired},
		{`{"error":"invalid_grant"}`, ErrDeviceInvalidGrant},
	}
	for _, tt := range tests {
		t.Run(tt.reply, func(t *testing.T) {
			var events []string
			c := pollServer(t, []string{pending, tt.reply}, &events)
			tok, err := c.PollDeviceToken(context.Background(), code(900, 5), &fakeClock{now: time.Unix(0, 0)})
			if !errors.Is(err, tt.want) || tok != "" {
				t.Fatalf("tok=%q err=%v", tok, err)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("not one line: %q", err)
			}
		})
	}
}

func TestPollDeviceTokenOtherServerErrorsNameTheError(t *testing.T) {
	for _, name := range []string{"unsupported_grant_type", "invalid_request", "server_error"} {
		var events []string
		c := pollServer(t, []string{`{"error":"` + name + `"}`}, &events)
		_, err := c.PollDeviceToken(context.Background(), code(900, 5), &fakeClock{now: time.Unix(0, 0)})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPollDeviceTokenStopsAtExpiresIn(t *testing.T) {
	var events []string
	replies := make([]string, 20)
	for i := range replies {
		replies[i] = pending
	}
	c := pollServer(t, replies, &events)
	clk := &fakeClock{now: time.Unix(0, 0)}
	_, err := c.PollDeviceToken(context.Background(), code(20, 5), clk)
	if !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("err = %v", err)
	}
	if len(events) != 3 {
		t.Errorf("polled %d times, want 3 (at 5s, 10s, 15s)", len(events))
	}
}

func TestPollDeviceTokenRateLimitWaitsRetryAfterOrInterval(t *testing.T) {
	var events []string
	c := pollServer(t, []string{"429", granted}, &events)
	clk := &fakeClock{now: time.Unix(0, 0)}
	if _, err := c.PollDeviceToken(context.Background(), code(900, 5), clk); err != nil {
		t.Fatal(err)
	}
	if len(clk.sleeps) != 2 || clk.sleeps[1] != 30*time.Second {
		t.Errorf("sleeps = %v, want second sleep 30s", clk.sleeps)
	}
}

func TestPollDeviceTokenCancelledContext(t *testing.T) {
	var events []string
	c := pollServer(t, []string{pending, pending}, &events)
	ctx, cancel := context.WithCancel(context.Background())
	clk := &cancelOnSleep{fakeClock: fakeClock{now: time.Unix(0, 0)}, cancel: cancel, at: 2}
	_, err := c.PollDeviceToken(ctx, code(900, 5), clk)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type cancelOnSleep struct {
	fakeClock
	cancel context.CancelFunc
	at     int
}

func (c *cancelOnSleep) Sleep(ctx context.Context, d time.Duration) error {
	if len(c.sleeps)+1 >= c.at {
		c.cancel()
	}
	return c.fakeClock.Sleep(ctx, d)
}

func TestValidateDeviceScopes(t *testing.T) {
	got, err := ValidateDeviceScopes([]string{"repo:read issues:write", "repo:read"})
	if err != nil || strings.Join(got, " ") != "repo:read issues:write" {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := ValidateDeviceScopes(nil); err != nil || strings.Join(got, " ") != "repo:write" {
		t.Errorf("default = %v, %v", got, err)
	}
	for _, bad := range []string{"repo:admin", "nope"} {
		_, err := ValidateDeviceScopes([]string{bad})
		if err == nil || !strings.Contains(err.Error(), bad) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}
