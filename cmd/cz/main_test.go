package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/cli"
)

type memKeyring struct {
	val  string
	fail bool
}

func (m *memKeyring) Set(v string) error {
	if m.fail {
		return errors.New("keychain unavailable")
	}
	m.val = v
	return nil
}

func (m *memKeyring) Get() (string, error) {
	if m.fail || m.val == "" {
		return "", cli.ErrNoEntry
	}
	return m.val, nil
}

func (m *memKeyring) Delete() error { m.val = ""; return nil }

type harness struct {
	app      *app
	kr       *memKeyring
	path     string
	env      map[string]string
	out      *bytes.Buffer
	errOut   *bytes.Buffer
	stdin    string
	srv      *httptest.Server
	lastReq  *http.Request
	lastBody string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		kr:     &memKeyring{},
		path:   filepath.Join(t.TempDir(), "cz", "credentials.json"),
		env:    map[string]string{},
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.lastReq, h.lastBody = r, string(b)
		switch {
		case r.Header.Get("Authorization") == "Bearer czp_good" && r.URL.Path == "/api/user":
			_, _ = io.WriteString(w, `{"id":3,"username":"ada"}`)
		case r.Header.Get("Authorization") == "Bearer czp_good":
			_, _ = io.WriteString(w, `[{"name":"demo"}]`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(h.srv.Close)
	h.app = &app{
		stdout: h.out,
		stderr: h.errOut,
		getenv: func(k string) string { return h.env[k] },
		newStore: func(insecure bool) (*cli.Store, error) {
			return &cli.Store{Keyring: h.kr, Path: h.path, Insecure: insecure}, nil
		},
	}
	return h
}

func (h *harness) run(args ...string) error {
	return h.runCtx(context.Background(), args...)
}

func (h *harness) runCtx(ctx context.Context, args ...string) error {
	h.out.Reset()
	h.errOut.Reset()
	h.app.stdin = strings.NewReader(h.stdin)
	cmd := newRootCmd(h.app)
	cmd.SetArgs(args)
	cmd.SetOut(h.errOut)
	cmd.SetErr(h.errOut)
	return cmd.ExecuteContext(ctx)
}

func TestLoginGoodTokenSavesAndStatusReportsUser(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_good\n"
	if err := h.run("auth", "login", "--host", h.srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String()+h.errOut.String(), "czp_good") {
		t.Error("login output leaks the token")
	}
	if h.kr.val == "" {
		t.Fatal("nothing saved to the keychain")
	}
	if err := h.run("auth", "status"); err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &st); err != nil {
		t.Fatalf("status not JSON: %v: %s", err, h.out.String())
	}
	if st["user"] != "ada" || st["host"] != h.srv.URL || st["storage"] != "keychain" {
		t.Errorf("status = %v", st)
	}
	if strings.Contains(h.out.String(), "czp_good") {
		t.Error("status leaks the token")
	}
}

func TestLoginBadTokenSavesNothing(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_bad\n"
	err := h.run("auth", "login", "--host", h.srv.URL, "--with-token")
	if err == nil {
		t.Fatal("want error")
	}
	if h.kr.val != "" {
		t.Error("bad token saved to keychain")
	}
	if _, statErr := os.Stat(h.path); !os.IsNotExist(statErr) {
		t.Error("bad token saved to file")
	}
	if strings.Contains(err.Error(), "czp_bad") {
		t.Error("error leaks the token")
	}
}

func TestLoginNeedsHost(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_good\n"
	if err := h.run("auth", "login", "--with-token"); err == nil || !strings.Contains(err.Error(), "--host") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnvCredentialsNeedNoLogin(t *testing.T) {
	h := newHarness(t)
	h.env["CZ_HOST"], h.env["CZ_TOKEN"] = h.srv.URL, "czp_good"
	if err := h.run("api", "GET", "/api/repos/"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(h.out.String()) != `[{"name":"demo"}]` {
		t.Errorf("out = %q", h.out.String())
	}
	if err := h.run("auth", "status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "environment") {
		t.Errorf("status = %s", h.out.String())
	}
}

func TestKeychainUnavailableFallsBackToFile0600(t *testing.T) {
	h := newHarness(t)
	h.kr.fail = true
	h.stdin = "czp_good\n"
	if err := h.run("auth", "login", "--host", h.srv.URL, "--with-token"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(h.path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("fi=%v err=%v", fi, err)
	}
	if err := h.run("auth", "status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "file") {
		t.Errorf("status = %s", h.out.String())
	}
}

func TestInsecureStorageForcesFile(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_good\n"
	if err := h.run("auth", "login", "--host", h.srv.URL, "--with-token", "--insecure-storage"); err != nil {
		t.Fatal(err)
	}
	if h.kr.val != "" {
		t.Error("keychain used")
	}
	if _, err := os.Stat(h.path); err != nil {
		t.Error(err)
	}
}

func TestStatusLoggedOutFails(t *testing.T) {
	h := newHarness(t)
	err := h.run("auth", "status")
	if err == nil || !strings.Contains(err.Error(), "cz auth login") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusHumanOutputOnTerminal(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_good\n"
	_ = h.run("auth", "login", "--host", h.srv.URL, "--with-token")
	h.app.stdoutTTY = true
	if err := h.run("auth", "status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "ada") || strings.HasPrefix(h.out.String(), "{") {
		t.Errorf("out = %q", h.out.String())
	}
	if err := h.run("auth", "status", "--json"); err != nil || !strings.HasPrefix(h.out.String(), "{") {
		t.Errorf("--json out = %q err=%v", h.out.String(), err)
	}
}

func TestLogoutRemovesLogin(t *testing.T) {
	h := newHarness(t)
	h.stdin = "czp_good\n"
	_ = h.run("auth", "login", "--host", h.srv.URL, "--with-token")
	if err := h.run("auth", "logout"); err != nil {
		t.Fatal(err)
	}
	if h.kr.val != "" {
		t.Error("keychain entry survives")
	}
	if err := h.run("auth", "status"); err == nil {
		t.Error("status succeeds after logout")
	}
}

func TestApiFieldsAndInput(t *testing.T) {
	h := newHarness(t)
	h.env["CZ_HOST"], h.env["CZ_TOKEN"] = h.srv.URL, "czp_good"

	if err := h.run("api", "POST", "/api/repos/", "--field", "name=x", "--field", "private=true"); err != nil {
		t.Fatal(err)
	}
	if h.lastReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", h.lastReq.Header.Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(h.lastBody), &body); err != nil || body["name"] != "x" || body["private"] != true {
		t.Errorf("body = %q err=%v", h.lastBody, err)
	}

	if err := h.run("api", "POST", "/api/repos/", "--form", "--field", "name=x y"); err != nil {
		t.Fatal(err)
	}
	if h.lastReq.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || h.lastBody != "name=x+y" {
		t.Errorf("form: %q %q", h.lastReq.Header.Get("Content-Type"), h.lastBody)
	}

	if err := h.run("api", "GET", "/api/repos/", "--field", "q=a b"); err != nil {
		t.Fatal(err)
	}
	if h.lastReq.URL.RawQuery != "q=a+b" || h.lastBody != "" {
		t.Errorf("get: query=%q body=%q", h.lastReq.URL.RawQuery, h.lastBody)
	}

	file := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(file, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.run("api", "PATCH", "/api/repos/o/r", "--input", file); err != nil {
		t.Fatal(err)
	}
	if h.lastReq.Method != "PATCH" || h.lastBody != `{"a":1}` {
		t.Errorf("input: %s %q", h.lastReq.Method, h.lastBody)
	}
}

func TestApiErrorIsOneLine(t *testing.T) {
	h := newHarness(t)
	h.env["CZ_HOST"], h.env["CZ_TOKEN"] = h.srv.URL, "czp_nope"
	err := h.run("api", "GET", "/api/repos/")
	if err == nil || !strings.Contains(err.Error(), "cz auth login") {
		t.Fatalf("err = %v", err)
	}
}

func TestVersionFlag(t *testing.T) {
	h := newHarness(t)
	cmd := newRootCmd(h.app)
	cmd.SetArgs([]string{"--version"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil || !strings.Contains(buf.String(), "cz version") {
		t.Fatalf("out=%q err=%v", buf.String(), err)
	}
}
