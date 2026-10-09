package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

func TestListPullsFiltersByStateOnTheClient(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos/ada/demo/pulls/" || r.URL.RawQuery != "" {
			t.Errorf("url = %s", r.URL)
		}
		_, _ = io.WriteString(w, `[{"number":2,"state":"open"},{"number":1,"state":"closed","extra":true}]`)
	})
	r := Repo{"ada", "demo"}
	for state, want := range map[string]int{"": 2, "all": 2, "open": 1, "closed": 1, "merged": 0} {
		got, err := c.ListPulls(context.Background(), r, state)
		if err != nil || len(got) != want {
			t.Errorf("state %q: %d pulls, %v", state, len(got), err)
		}
	}
	got, _ := c.ListPulls(context.Background(), r, "closed")
	if !strings.Contains(string(got[0].Raw), `"extra":true`) {
		t.Errorf("raw = %s", got[0].Raw)
	}
}

func TestMergePullTurnsScope403IntoRepoWriteAdvice(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="repo:write"`)
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := c.MergePull(context.Background(), Repo{"ada", "demo"}, 7, "ff")
	var e *Error
	if !errors.As(err, &e) || e.Status != 403 || !strings.Contains(e.Msg, "merging needs") {
		t.Fatalf("err = %v", err)
	}
}

func TestMergePullKeepsOtherForbiddenMessages(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"repository is archived"}`)
	})
	_, err := c.MergePull(context.Background(), Repo{"ada", "demo"}, 7, "ff")
	if err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("err = %v", err)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "topic").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	if got, err := CurrentBranch(dir); err != nil || got != "topic" {
		t.Errorf("branch = %q, %v", got, err)
	}
	if _, err := CurrentBranch(t.TempDir()); err == nil || !strings.Contains(err.Error(), "--head") {
		t.Errorf("non-checkout err = %v", err)
	}
}
