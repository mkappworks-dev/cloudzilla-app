package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type testClock struct {
	now     time.Time
	sleeps  []time.Duration
	onSleep func(n int)
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	if c.onSleep != nil {
		c.onSleep(len(c.sleeps))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	return nil
}

// deviceHarness is a fake Cloudzilla that answers the device endpoints from a script.
type deviceHarness struct {
	*harness
	dev      *httptest.Server
	replies  []string // one per poll: a JSON body; one holding access_token is a 200
	codeBody string
	codeCode int
	forms    []url.Values
	codeHits int
	polls    int
	userOK   bool
	clock    *testClock
	opened   []string
	copied   []string
	hostname string
}

const defaultCodeBody = `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri":"%URL%/login/device","expires_in":900,"interval":5}`

func newDeviceHarness(t *testing.T, replies ...string) *deviceHarness {
	t.Helper()
	d := &deviceHarness{harness: newHarness(t), replies: replies, codeCode: 200, userOK: true, hostname: "mk-laptop"}
	d.dev = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/auth/device/code":
			d.codeHits++
			v, _ := url.ParseQuery(string(b))
			d.forms = append(d.forms, v)
			body := d.codeBody
			if body == "" {
				body = strings.ReplaceAll(defaultCodeBody, "%URL%", "http://"+r.Host)
			}
			w.WriteHeader(d.codeCode)
			_, _ = io.WriteString(w, body)
		case "/api/auth/device/token":
			if d.polls >= len(d.replies) {
				t.Errorf("unexpected poll %d", d.polls+1)
				w.WriteHeader(500)
				return
			}
			body := d.replies[d.polls]
			d.polls++
			if strings.Contains(body, "access_token") {
				_, _ = io.WriteString(w, body)
				return
			}
			w.WriteHeader(400)
			_, _ = io.WriteString(w, body)
		case "/api/user":
			if r.Header.Get("Authorization") == "Bearer czp_new" && d.userOK {
				_, _ = io.WriteString(w, `{"id":3,"username":"ada"}`)
				return
			}
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(d.dev.Close)
	d.clock = &testClock{now: time.Unix(0, 0)}
	d.app.clock = d.clock
	d.app.hostname = func() (string, error) { return d.hostname, nil }
	d.app.openBrowser = func(u string) error { d.opened = append(d.opened, u); return nil }
	d.app.copyClipboard = func(s string) error { d.copied = append(d.copied, s); return nil }
	return d
}

func (d *deviceHarness) login(extra ...string) error {
	return d.run(append([]string{"auth", "login", "--host", d.dev.URL}, extra...)...)
}

func (d *deviceHarness) assertNothingStored(t *testing.T) {
	t.Helper()
	if d.kr.val != "" {
		t.Error("token saved to keychain")
	}
	if _, err := os.Stat(d.path); !os.IsNotExist(err) {
		t.Error("token saved to file")
	}
}

const (
	devPending = `{"error":"authorization_pending"}`
	devSlow    = `{"error":"slow_down"}`
	devGranted = `{"access_token":"czp_new","token_type":"bearer","scope":"repo:write"}`
)

func TestDeviceLoginPendingThenSuccessStoresToken(t *testing.T) {
	d := newDeviceHarness(t, devPending, devPending, devGranted)
	d.app.stdoutTTY = true
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if d.kr.val == "" {
		t.Fatal("token not stored")
	}
	if got := d.out.String(); !strings.Contains(got, "Logged in to "+d.dev.URL+" as ada") {
		t.Errorf("out = %q", got)
	}
	if strings.Contains(d.out.String()+d.errOut.String(), "czp_new") {
		t.Error("output leaks the token")
	}
	if err := d.run("auth", "status"); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceLoginSleepsAfterEachResponseAndHonoursSlowDown(t *testing.T) {
	d := newDeviceHarness(t, devSlow, devPending, devGranted)
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(d.clock.sleeps) != len(want) {
		t.Fatalf("sleeps = %v", d.clock.sleeps)
	}
	for i := range want {
		if d.clock.sleeps[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v", d.clock.sleeps, want)
		}
	}
}

func TestDeviceLoginPrintsURLAndCodeWithoutTTYAndDoesNotOpen(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{d.dev.URL + "/login/device", "ABCD-EFGH"} {
		if !strings.Contains(d.errOut.String(), want) {
			t.Errorf("stderr lacks %q: %s", want, d.errOut.String())
		}
	}
	if len(d.opened) != 0 {
		t.Errorf("opened %v without a TTY", d.opened)
	}
}

func TestDeviceLoginNeedsBothStreamsToBeTerminalsToOpen(t *testing.T) {
	for _, tc := range []struct{ in, out bool }{{true, false}, {false, true}} {
		d := newDeviceHarness(t, devGranted)
		d.app.stdinTTY, d.app.stdoutTTY = tc.in, tc.out
		if err := d.login(); err != nil {
			t.Fatal(err)
		}
		if len(d.opened) != 0 {
			t.Errorf("in=%v out=%v opened %v", tc.in, tc.out, d.opened)
		}
	}
}

func TestDeviceLoginOpensPlainURLOnTerminalAndCopiesCode(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.app.stdinTTY, d.app.stdoutTTY = true, true
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if len(d.opened) != 1 || d.opened[0] != d.dev.URL+"/login/device" {
		t.Fatalf("opened = %v", d.opened)
	}
	for _, u := range append(d.opened, d.errOut.String()) {
		if strings.Contains(u, "user_code") {
			t.Errorf("a URL carries the code: %s", u)
		}
	}
	if len(d.copied) != 1 || d.copied[0] != "ABCD-EFGH" {
		t.Errorf("copied = %v", d.copied)
	}
}

func TestDeviceLoginNoBrowserSuppressesOpening(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.app.stdinTTY, d.app.stdoutTTY = true, true
	if err := d.login("--no-browser"); err != nil {
		t.Fatal(err)
	}
	if len(d.opened) != 0 {
		t.Errorf("opened %v", d.opened)
	}
	if !strings.Contains(d.errOut.String(), "ABCD-EFGH") {
		t.Errorf("stderr = %s", d.errOut.String())
	}
}

func TestDeviceLoginDoesNotOpenNonHTTPURI(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.codeBody = `{"device_code":"dc","user_code":"ABCD-EFGH","verification_uri":"file:///etc/passwd","expires_in":900,"interval":5}`
	d.app.stdinTTY, d.app.stdoutTTY = true, true
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if len(d.opened) != 0 {
		t.Errorf("opened %v", d.opened)
	}
}

func TestDeviceLoginSurvivesMissingClipboardAndFailingOpener(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.app.stdinTTY, d.app.stdoutTTY = true, true
	d.app.copyClipboard = func(string) error { return io.ErrClosedPipe }
	d.app.openBrowser = func(string) error { return io.ErrClosedPipe }
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.errOut.String(), "clipboard") {
		t.Errorf("claims a copy that failed: %s", d.errOut.String())
	}
}

func TestDeviceLoginTerminalErrorsStoreNothing(t *testing.T) {
	for name, tc := range map[string]struct{ reply, want string }{
		"denied":  {`{"error":"access_denied"}`, "denied"},
		"expired": {`{"error":"expired_token"}`, "expired"},
		"invalid": {`{"error":"invalid_grant"}`, "no longer valid"},
	} {
		t.Run(name, func(t *testing.T) {
			d := newDeviceHarness(t, devPending, tc.reply)
			err := d.login()
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "\n") {
				t.Fatalf("err = %v", err)
			}
			d.assertNothingStored(t)
		})
	}
}

func TestDeviceLoginCtrlCCancelsAndStoresNothing(t *testing.T) {
	d := newDeviceHarness(t, devPending, devPending)
	ctx, cancel := context.WithCancel(context.Background())
	d.clock.onSleep = func(n int) {
		if n == 2 {
			cancel()
		}
	}
	err := d.runCtx(ctx, "auth", "login", "--host", d.dev.URL)
	if err == nil || !strings.Contains(err.Error(), "cancel") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %v", err)
	}
	d.assertNothingStored(t)
}

func TestDeviceLoginOlderServerGetsWithTokenHint(t *testing.T) {
	d := newDeviceHarness(t)
	d.codeCode, d.codeBody = 404, `404 page not found`
	err := d.login()
	if err == nil || !strings.Contains(err.Error(), "--with-token") {
		t.Fatalf("err = %v", err)
	}
	d.assertNothingStored(t)
}

func TestDeviceLoginSendsRequestedScopeAndHostname(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	if err := d.login("--scope", "repo:read", "--scope", "issues:write pulls:write"); err != nil {
		t.Fatal(err)
	}
	if len(d.forms) != 1 {
		t.Fatalf("forms = %v", d.forms)
	}
	if got := d.forms[0].Get("scope"); got != "repo:read issues:write pulls:write" {
		t.Errorf("scope = %q", got)
	}
	if got := d.forms[0].Get("device_name"); got != "mk-laptop" {
		t.Errorf("device_name = %q", got)
	}
}

func TestDeviceLoginDefaultScopeIsRepoWrite(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if got := d.forms[0].Get("scope"); got != "repo:write" {
		t.Errorf("scope = %q", got)
	}
}

func TestDeviceLoginRefusesAdminAndUnknownScopeBeforeAnyRequest(t *testing.T) {
	for _, sc := range []string{"repo:admin", "bogus"} {
		d := newDeviceHarness(t)
		err := d.login("--scope", sc)
		if err == nil || !strings.Contains(err.Error(), sc) || strings.Contains(err.Error(), "\n") {
			t.Fatalf("%s: err = %v", sc, err)
		}
		if d.codeHits != 0 {
			t.Errorf("%s: %d requests sent", sc, d.codeHits)
		}
	}
}

func TestDeviceLoginUnverifiableTokenStoresNothing(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.userOK = false
	if err := d.login(); err == nil {
		t.Fatal("want error")
	}
	d.assertNothingStored(t)
}

func TestDeviceLoginSendsNoHostnameWhenUnknown(t *testing.T) {
	d := newDeviceHarness(t, devGranted)
	d.app.hostname = func() (string, error) { return "", io.ErrUnexpectedEOF }
	if err := d.login(); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.forms[0]["device_name"]; ok {
		t.Errorf("form = %v", d.forms[0])
	}
}

func TestWithTokenSkipsDeviceFlow(t *testing.T) {
	d := newDeviceHarness(t)
	d.stdin = "czp_good\n"
	if err := d.run("auth", "login", "--host", d.srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}
	if d.codeHits != 0 {
		t.Error("device flow ran with --with-token")
	}
}
