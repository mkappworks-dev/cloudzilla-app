package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repoHarness struct {
	*harness
	api     *httptest.Server
	reqs    []string
	bodies  []string
	gitEnv  []string
	gitArgs []string
	gitRuns int
}

func newRepoHarness(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *repoHarness {
	t.Helper()
	rh := &repoHarness{harness: newHarness(t)}
	rh.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rh.reqs = append(rh.reqs, r.Method+" "+r.URL.Path)
		rh.bodies = append(rh.bodies, string(b))
		if r.Header.Get("Authorization") != "Bearer czp_good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f, ok := routes[r.Method+" "+r.URL.Path]; ok {
			f(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(rh.api.Close)
	rh.env["CZ_HOST"], rh.env["CZ_TOKEN"] = rh.api.URL, "czp_good"
	rh.app.runGit = func(ctx context.Context, env []string, args ...string) error {
		rh.gitRuns++
		rh.gitEnv, rh.gitArgs = env, args
		return nil
	}
	return rh
}

func reply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

const repoJSON = `{"owner_name":"ada","name":"demo","description":"A demo","private":true,"default_branch":"main","fork_count":2,"updated_at":"2026-10-01T00:00:00Z"}`

func TestRepoListJSONWhenPipedAndTableOnTerminal(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/": reply(200, `[`+repoJSON+`,{"owner_name":"acme","name":"site","description":"","private":false}]`),
	})
	if err := h.run("repo", "list", "--owner", "ada"); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil || len(got) != 1 || got[0]["name"] != "demo" {
		t.Fatalf("piped output = %s (%v)", h.out.String(), err)
	}

	h.app.stdoutTTY = true
	if err := h.run("repo", "list"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	for _, want := range []string{"NAME", "ada/demo", "private", "acme/site", "public"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestRepoListEmptyIsAnEmptyArray(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){"GET /api/repos/": reply(200, `[]`)})
	if err := h.run("repo", "list"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(h.out.String()) != "[]" {
		t.Errorf("output = %q", h.out.String())
	}
}

func TestRepoViewByArgAndByOrigin(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/repos/ada/demo": reply(200, repoJSON),
	})
	h.app.stdoutTTY = true
	if err := h.run("repo", "view", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ada/demo", "A demo", "main", "private"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("view lacks %q:\n%s", want, h.out.String())
		}
	}

	h.app.getwd = func() (string, error) { return remoteCheckout(t, "git@"+hostOnly(h.api.URL)+":ada/demo.git"), nil }
	h.reqs = nil
	if err := h.run("repo", "view"); err != nil {
		t.Fatal(err)
	}
	if len(h.reqs) != 1 || h.reqs[0] != "GET /api/repos/ada/demo" {
		t.Errorf("requests = %v", h.reqs)
	}
	if err := h.run("repo", "view", "-R", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("repo", "view", "ada/demo", "-R", "ada/other"); err == nil {
		t.Error("positional and -R together should fail")
	}
}

func TestRepoViewFromForeignOriginSaysSo(t *testing.T) {
	h := newRepoHarness(t, nil)
	h.app.getwd = func() (string, error) { return remoteCheckout(t, "https://github.com/ada/demo.git"), nil }
	err := h.run("repo", "view")
	if err == nil || !strings.Contains(err.Error(), "github.com") || !strings.Contains(err.Error(), "not") {
		t.Errorf("err = %v", err)
	}
	if len(h.reqs) != 0 {
		t.Errorf("requested %v despite the foreign remote", h.reqs)
	}
}

func TestRepoCreateSendsOptions(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/": reply(201, repoJSON),
	})
	h.app.stdoutTTY = true
	err := h.run("repo", "create", "demo", "--private", "-d", "A demo", "--readme", "--gitignore", "Go", "--license", "mit")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(h.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "demo" || body["private"] != true || body["add_readme"] != true || body["gitignore"] != "Go" || body["license"] != "mit" || body["description"] != "A demo" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(h.out.String(), "ada/demo") {
		t.Errorf("output = %q", h.out.String())
	}
}

func TestRepoCreateInOrg(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/orgs/acme/repos": reply(201, `{"owner_name":"acme","name":"site"}`),
	})
	if err := h.run("repo", "create", "site", "--org", "acme", "--private=false"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.bodies[0], `"private":false`) {
		t.Errorf("explicit --private=false not sent: %s", h.bodies[0])
	}
}

func TestRepoCreateDuplicateNamePrintsServerMessage(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/": reply(422, `{"error":"a repository with that name already exists"}`),
	})
	err := h.run("repo", "create", "demo")
	if err == nil || !strings.Contains(err.Error(), "a repository with that name already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestRepoCreateReadOnlyTokenGetsScopeMessage(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="repo:write"`)
			w.WriteHeader(403)
		},
	})
	err := h.run("repo", "create", "demo")
	if err == nil || !strings.Contains(err.Error(), "repo:write") {
		t.Errorf("err = %v", err)
	}
}

func TestRepoFork(t *testing.T) {
	h := newRepoHarness(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/repos/ada/demo/fork": reply(201, `{"owner":"bob","name":"demo","url":"/bob/demo"}`),
	})
	h.app.stdoutTTY = true
	if err := h.run("repo", "fork", "ada/demo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "bob/demo") {
		t.Errorf("output = %q", h.out.String())
	}
}

func TestRepoCloneKeepsTokenOutOfArgsAndURL(t *testing.T) {
	h := newRepoHarness(t, nil)
	if err := h.run("repo", "clone", "ada/demo", "work", "--", "--depth", "1"); err != nil {
		t.Fatal(err)
	}
	if len(h.reqs) != 0 {
		t.Errorf("clone made API requests: %v", h.reqs)
	}
	joined := strings.Join(h.gitArgs, " ")
	if strings.Contains(joined, "czp_good") {
		t.Errorf("token in git args: %s", joined)
	}
	if !strings.Contains(joined, h.api.URL+"/ada/demo.git") || strings.Contains(joined, "@") && strings.Contains(joined, "://czp") {
		t.Errorf("args = %s", joined)
	}
	if !strings.HasSuffix(joined, "work --depth 1") && !strings.Contains(joined, "--depth 1 "+h.api.URL) {
		t.Errorf("destination or passthrough flags missing: %s", joined)
	}
	if !contains(h.gitEnv, "CZ_GIT_TOKEN=czp_good") {
		t.Errorf("token not passed through the environment: %v", h.gitEnv)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestRepoCloneRejectsForeignHost(t *testing.T) {
	h := newRepoHarness(t, nil)
	if err := h.run("repo", "clone", "https://github.com/ada/demo"); err == nil {
		t.Error("cloned a repository from another host with this host's token")
	}
	if h.gitRuns != 0 {
		t.Error("git ran")
	}
}

// Drives the real git against a stub smart-HTTP server to prove the helper supplies the token and the clone keeps no trace of it.
func TestRepoCloneWithRealGitLeavesNoToken(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuth = user + ":" + pass
		if r.URL.Path != "/ada/demo.git/info/refs" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		zero := strings.Repeat("0", 40)
		line := zero + " capabilities^{}\x00agent=stub\n"
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000"+pktLen(line)+line+"0000")
	}))
	defer srv.Close()

	h := newHarness(t)
	h.env["CZ_HOST"], h.env["CZ_TOKEN"] = srv.URL, "czp_good"
	h.app.runGit = nil
	dest := filepath.Join(t.TempDir(), "work")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := h.run("repo", "clone", "ada/demo", dest); err != nil {
		t.Fatalf("clone: %v\n%s", err, h.errOut.String())
	}
	if !strings.HasSuffix(sawAuth, ":czp_good") {
		t.Errorf("server saw credentials %q", sawAuth)
	}
	cfg, err := os.ReadFile(filepath.Join(dest, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "czp_good") || strings.Contains(string(cfg), "credential") {
		t.Errorf(".git/config keeps credentials:\n%s", cfg)
	}
}

func pktLen(s string) string {
	const hex = "0123456789abcdef"
	n := len(s) + 4
	return string([]byte{hex[n>>12&15], hex[n>>8&15], hex[n>>4&15], hex[n&15]})
}

func hostOnly(u string) string {
	u = strings.TrimPrefix(u, "http://")
	host, _, _ := strings.Cut(u, ":")
	return host
}

// remoteCheckout returns a fresh git checkout whose origin is remote.
func remoteCheckout(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	return dir
}
