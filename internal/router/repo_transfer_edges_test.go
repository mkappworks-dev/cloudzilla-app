package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRepoTransfer_RoutesRequireSignIn(t *testing.T) {
	e := newTransferEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/user/transfers"},
		{http.MethodPost, "/api/user/transfers/1/accept"},
		{http.MethodPost, "/api/user/transfers/1/decline"},
		{http.MethodDelete, "/api/repos/someone/somerepo/transfer"},
	} {
		if rr := serve(e.h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestRepoTransfer_UnknownIDsAre404(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	for _, path := range []string{
		"/api/user/transfers/abc/accept", "/api/user/transfers/abc/decline",
		"/api/user/transfers/999999999/accept", "/api/user/transfers/999999999/decline",
	} {
		if rr := serve(e.h, browserRequest(http.MethodPost, path, me.session, url.Values{"repo": {"a/b"}})); rr.Code != http.StatusNotFound {
			t.Errorf("POST %s: got %d, want 404", path, rr.Code)
		}
	}
}

func TestRepoTransfer_AcceptWithoutHTMXReturnsTheRepo(t *testing.T) {
	e := newTransferEnv(t)
	owner, recipient := e.account(t), e.account(t)
	repo, transfer := e.offer(t, owner, recipient)

	rr := serve(e.h, browserRequest(http.MethodPost, transferPath(transfer, "accept"), recipient.session, url.Values{"repo": {transfer.FullName()}}))
	var got model.Repository
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &got) != nil || got.ID != repo.ID || got.OwnerName != recipient.name {
		t.Fatalf("accept: got %d %s, want 200 with the repo now owned by the recipient", rr.Code, rr.Body)
	}
	if owner := e.ownerOf(t, repo.ID); owner != recipient.id {
		t.Errorf("owner = %d, want the recipient", owner)
	}
}

func TestRepoTransfer_CancelRefusals(t *testing.T) {
	e := newTransferEnv(t)
	owner, stranger := e.account(t), e.account(t)
	repo, _ := e.offer(t, owner, e.account(t))
	cancel := "/api/repos/" + owner.name + "/" + repo.Name + "/transfer"

	if rr := serve(e.h, browserRequest(http.MethodDelete, "/api/repos/"+owner.name+"/missing_repo/transfer", owner.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("cancel on a missing repo: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, cancel, stranger.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("stranger cancelling a private repo's transfer: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, htmxRequest(browserRequest(http.MethodDelete, cancel, owner.session, nil))); rr.Code != http.StatusNoContent || rr.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("owner cancelling over HTMX: got %d (HX-Refresh %q), want 204 and a refresh", rr.Code, rr.Header().Get("HX-Refresh"))
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, cancel, owner.session, nil)); rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "no pending transfer") {
		t.Errorf("cancelling twice: got %d %s, want 404 no pending transfer", rr.Code, rr.Body)
	}
}

func TestRepoTransfer_PublicRepoCancelIsOwnerOnly(t *testing.T) {
	e := newTransferEnv(t)
	owner, recipient, stranger := e.account(t), e.account(t), e.account(t)
	repo, _ := e.offer(t, owner, recipient)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = false WHERE id = $1`, repo.ID)
	cancel := "/api/repos/" + owner.name + "/" + repo.Name + "/transfer"

	if rr := serve(e.h, browserRequest(http.MethodDelete, cancel, stranger.session, nil)); rr.Code != http.StatusForbidden {
		t.Errorf("stranger cancelling a public repo's transfer: got %d, want 403", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM repo_transfers WHERE repo_id = $1`, repo.ID); n != 1 {
		t.Error("a refused cancel removed the transfer")
	}
}
