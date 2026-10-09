package handler_test

import (
	"net/http"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCreateRepo_RefusedWith403AtTheCountQuota(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouterWithQuota(t, db, t.TempDir(), config.QuotaConfig{User: config.QuotaLimits{Repos: 1}})
	r := seedOwnedRepo(t, db, false)

	rr := requestAPIBody(h, http.MethodPost, "/api/repos", r.owner.token, `{"name":"second"}`)

	if want := `{"error":"repository quota reached (1 of 1)"}` + "\n"; rr.Code != http.StatusForbidden || rr.Body.String() != want {
		t.Errorf("want 403 %q, got %d %q", want, rr.Code, rr.Body.String())
	}
}

func TestCreateRepo_UnchangedWithEveryQuotaAtZero(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newAPIRouterWithQuota(t, db, t.TempDir(), config.QuotaConfig{})
	r := seedOwnedRepo(t, db, false)

	rr := requestAPIBody(h, http.MethodPost, "/api/repos", r.owner.token, `{"name":"second"}`)

	if rr.Code != http.StatusCreated {
		t.Errorf("want 201, got %d %q", rr.Code, rr.Body.String())
	}
}
