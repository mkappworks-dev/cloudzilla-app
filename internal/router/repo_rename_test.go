package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRepoRename_PatchNameThroughTheRouter(t *testing.T) {
	e := newTransferEnv(t)
	owner, admin, writer, stranger := e.account(t), e.account(t), e.account(t), e.account(t)
	repo, err := e.svc.Repo.Create(context.Background(), owner.id, owner.name, "before_"+testutil.UniqueSuffix(t), "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	for who, role := range map[string]string{admin.name: "admin", writer.name: "writer"} {
		if err := e.svc.Repo.AddCollaborator(context.Background(), repo, owner.id, who, role); err != nil {
			t.Fatal(err)
		}
	}
	path := func() string { return "/api/repos/" + owner.name + "/" + currentName(t, e, repo.ID) }
	patch := func(session string, form url.Values) int {
		return serve(e.h, browserRequest(http.MethodPatch, path(), session, form)).Code
	}
	confirmed := func(name string) url.Values { return url.Values{"name": {name}, "password": {"password1"}} }

	if got := patch(stranger.session, confirmed("x_"+owner.name)); got != http.StatusNotFound {
		t.Errorf("stranger on a private repo: got %d, want 404", got)
	}
	if got := patch(writer.session, confirmed("by_writer")); got != http.StatusForbidden {
		t.Errorf("writer: got %d, want 403", got)
	}
	if got := patch(owner.session, url.Values{"name": {"no_stepup"}}); got == http.StatusOK {
		t.Errorf("rename without the password: got %d, want a refusal", got)
	}
	mixed := confirmed("mixed")
	mixed.Set("description", "also this")
	if got := patch(owner.session, mixed); got != http.StatusUnprocessableEntity {
		t.Errorf("name with description: got %d, want 422", got)
	}
	for name, want := range map[string]int{
		"bad name":                 http.StatusUnprocessableEntity,
		"has.wiki":                 http.StatusUnprocessableEntity,
		currentName(t, e, repo.ID): http.StatusUnprocessableEntity,
	} {
		if got := patch(owner.session, confirmed(name)); got != want {
			t.Errorf("rename to %q: got %d, want %d", name, got, want)
		}
	}
	e.svc.Repo.Create(context.Background(), owner.id, owner.name, "occupied", "", false, service.RepoInitOptions{})
	if got := patch(owner.session, confirmed("occupied")); got != http.StatusConflict {
		t.Errorf("taken name: got %d, want 409", got)
	}

	old := currentName(t, e, repo.ID)
	rr := serve(e.h, browserRequest(http.MethodPatch, path(), admin.session, confirmed("renamed_"+testutil.UniqueSuffix(t))))
	var body struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &body) != nil || body.ID != repo.ID || body.FullName != owner.name+"/"+currentName(t, e, repo.ID) {
		t.Fatalf("admin rename: got %d %s", rr.Code, rr.Body)
	}
	if currentName(t, e, repo.ID) == old {
		t.Error("the repo kept its name")
	}

	deadline := time.Now().Add(2 * time.Second)
	for countRows(t, e.db, `SELECT COUNT(*) FROM audit_log WHERE action = 'repo.rename' AND target_id = $1 AND metadata->>'from' = $2`, repo.ID, old) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no repo.rename audit entry with the old name")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func currentName(t *testing.T, e transferEnv, repoID int64) string {
	t.Helper()
	var name string
	if err := e.db.QueryRow(`SELECT name FROM repositories WHERE id = $1`, repoID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}
