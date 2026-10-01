package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type transferAccount struct {
	id            int64
	name, session string
}

type transferEnv struct {
	h   http.Handler
	db  *sql.DB
	svc *service.Services
}

func newTransferEnv(t *testing.T) transferEnv {
	h, svc, db := newVerificationRouter(t, config.SMTPConfig{})
	return transferEnv{h: h, db: db, svc: svc}
}

func (e transferEnv) account(t *testing.T) transferAccount {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id, _ := testutil.SeedUserWithPassword(t, e.db, suffix, "password1")
	return transferAccount{id, "testpw_" + suffix, makeJWT(t, id, "testpw_"+suffix)}
}

// offer creates a private repo for from and requests its transfer to to over HTTP.
func (e transferEnv) offer(t *testing.T, from, to transferAccount) (*model.Repository, model.RepoTransfer) {
	t.Helper()
	repo, err := e.svc.Repo.Create(context.Background(), from.id, from.name, "offered_"+testutil.UniqueSuffix(t), "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	rr := serve(e.h, browserRequest(http.MethodPost, "/api/repos/"+from.name+"/"+repo.Name+"/transfer", from.session,
		url.Values{"new_owner": {to.name}, "password": {"password1"}}))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("request transfer: got %d %s, want 202", rr.Code, rr.Body)
	}
	var transfer model.RepoTransfer
	if err := json.Unmarshal(rr.Body.Bytes(), &transfer); err != nil {
		t.Fatalf("decode transfer: %v", err)
	}
	return repo, transfer
}

func (e transferEnv) ownerOf(t *testing.T, repoID int64) int64 {
	t.Helper()
	var ownerID int64
	if err := e.db.QueryRow(`SELECT owner_id FROM repositories WHERE id = $1`, repoID).Scan(&ownerID); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	return ownerID
}

func transferPath(transfer model.RepoTransfer, action string) string {
	return "/api/user/transfers/" + strconv.FormatInt(transfer.ID, 10) + "/" + action
}

func TestRepoTransfer_RecipientIsNotifiedAndAccepts(t *testing.T) {
	e := newTransferEnv(t)
	owner, recipient, stranger := e.account(t), e.account(t), e.account(t)
	repo, transfer := e.offer(t, owner, recipient)

	if transfer.RecipientID != recipient.id || transfer.FullName() != owner.name+"/"+repo.Name {
		t.Errorf("transfer = %+v", transfer)
	}
	if got := e.ownerOf(t, repo.ID); got != owner.id {
		t.Fatalf("the repo moved to user %d before anyone accepted", got)
	}

	notified := func() int {
		return countRows(t, e.db, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'repo_transfer' AND subject_id = $2`, recipient.id, transfer.ID)
	}
	// The notification is written by a goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for notified() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := notified(); n != 1 {
		t.Errorf("recipient has %d transfer notifications, want 1", n)
	}

	settings := serve(e.h, browserRequest(http.MethodGet, "/"+owner.name+"/"+repo.Name+"/settings", owner.session, nil))
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), "Waiting for") || !strings.Contains(settings.Body.String(), "@"+recipient.name) {
		t.Errorf("repo settings don't show the pending transfer: %d", settings.Code)
	}
	page := serve(e.h, browserRequest(http.MethodGet, "/repos/transfers", recipient.session, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), transfer.FullName()) {
		t.Errorf("transfers page: got %d, want it to list %s", page.Code, transfer.FullName())
	}
	if page := serve(e.h, browserRequest(http.MethodGet, "/repos/transfers", stranger.session, nil)); strings.Contains(page.Body.String(), transfer.FullName()) {
		t.Error("another user's transfers page lists the transfer")
	}
	list := serve(e.h, browserRequest(http.MethodGet, "/api/user/transfers", recipient.session, nil))
	var listed []model.RepoTransfer
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != transfer.ID {
		t.Errorf("GET /api/user/transfers = %d %s", list.Code, list.Body)
	}

	accept := func(a transferAccount, offered string) int {
		req := htmxRequest(browserRequest(http.MethodPost, transferPath(transfer, "accept"), a.session, url.Values{"repo": {offered}}))
		rr := serve(e.h, req)
		if rr.Code == http.StatusNoContent && rr.Header().Get("HX-Redirect") != "/"+recipient.name+"/"+repo.Name {
			t.Errorf("accept redirects to %q", rr.Header().Get("HX-Redirect"))
		}
		return rr.Code
	}
	if code := accept(stranger, transfer.FullName()); code != http.StatusNotFound {
		t.Errorf("stranger accepting: got %d, want 404", code)
	}
	if code := accept(recipient, recipient.name+"/"+repo.Name); code != http.StatusConflict {
		t.Errorf("accepting a name the recipient was not shown: got %d, want 409", code)
	}
	if got := e.ownerOf(t, repo.ID); got != owner.id {
		t.Fatalf("a refused accept moved the repo to user %d", got)
	}
	if code := accept(recipient, transfer.FullName()); code != http.StatusNoContent {
		t.Fatalf("recipient accepting: got %d, want 204", code)
	}
	if got := e.ownerOf(t, repo.ID); got != recipient.id {
		t.Errorf("repo owner after acceptance = %d, want %d", got, recipient.id)
	}
}

func TestRepoTransfer_DeclineAndCancelMoveNothing(t *testing.T) {
	e := newTransferEnv(t)
	owner, recipient := e.account(t), e.account(t)

	declined, transfer := e.offer(t, owner, recipient)
	rr := serve(e.h, htmxRequest(browserRequest(http.MethodPost, transferPath(transfer, "decline"), recipient.session, url.Values{})))
	if rr.Code != http.StatusNoContent || rr.Header().Get("HX-Refresh") != "true" {
		t.Errorf("decline: got %d (HX-Refresh %q), want 204 and a refresh", rr.Code, rr.Header().Get("HX-Refresh"))
	}
	rr = serve(e.h, browserRequest(http.MethodPost, transferPath(transfer, "accept"), recipient.session, url.Values{"repo": {transfer.FullName()}}))
	if rr.Code != http.StatusNotFound {
		t.Errorf("accept after decline: got %d, want 404", rr.Code)
	}
	if got := e.ownerOf(t, declined.ID); got != owner.id {
		t.Errorf("declined repo moved to user %d", got)
	}

	cancelled, transfer := e.offer(t, owner, recipient)
	cancel := "/api/repos/" + owner.name + "/" + cancelled.Name + "/transfer"
	if rr := serve(e.h, browserRequest(http.MethodDelete, cancel, recipient.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("recipient cancelling a private repo's transfer: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, cancel, owner.session, nil)); rr.Code != http.StatusNoContent {
		t.Errorf("owner cancelling: got %d %s, want 204", rr.Code, rr.Body)
	}
	rr = serve(e.h, browserRequest(http.MethodPost, transferPath(transfer, "accept"), recipient.session, url.Values{"repo": {transfer.FullName()}}))
	if rr.Code != http.StatusNotFound {
		t.Errorf("accept after cancel: got %d, want 404", rr.Code)
	}
	if got := e.ownerOf(t, cancelled.ID); got != owner.id {
		t.Errorf("cancelled repo moved to user %d", got)
	}
}
