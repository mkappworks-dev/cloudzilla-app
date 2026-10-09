package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestWebWrites_RefusedWhenTheStorageQuotaIsFull(t *testing.T) {
	tests := []struct {
		name string
		send func(t *testing.T, h http.Handler, r raceRepo) (status int, body string)
	}{
		{"new file", func(t *testing.T, h http.Handler, r raceRepo) (int, string) {
			rr := postForm(t, h, r.owner.token, r.path+"/new/main", url.Values{"path": {"added.txt"}, "content": {"hi"}})
			return rr.Code, rr.Body.String()
		}},
		{"wiki page", func(t *testing.T, h http.Handler, r raceRepo) (int, string) {
			rr := postForm(t, h, r.owner.token, "/api/repos"+r.path+"/wiki/Home", url.Values{"content": {"hi"}})
			return rr.Code, rr.Body.String()
		}},
		{"wiki page order", func(t *testing.T, h http.Handler, r raceRepo) (int, string) {
			rr := postForm(t, h, r.owner.token, "/api/repos"+r.path+"/wiki/order", url.Values{"order": {"Home"}})
			return rr.Code, rr.Body.String()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			h := newAPIRouterWithQuota(t, db, reposRoot, userStorageQuota)
			r := seedRaceRepo(t, db, reposRoot)
			testutil.Exec(t, db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, httpQuotaBytes)

			status, body := tt.send(t, h, r)

			if status != http.StatusForbidden || !strings.Contains(body, "storage quota reached (9.8 KiB of 9.8 KiB)") {
				t.Errorf("want 403 and the quota message, got %d %q", status, body)
			}
			assertRef(t, r, "main", r.mainTip)
		})
	}
}

func TestWebWrites_NewFileUnderTheQuotaCommitsAndRemeasures(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1 << 30}})
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = 1 WHERE id = $1`, r.id)

	rr := postForm(t, h, r.owner.token, r.path+"/new/main", url.Values{"path": {"added.txt"}, "content": {"hi"}})

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want a redirect after the commit, got %d %q", rr.Code, rr.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var size int64
		if err := db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, r.id).Scan(&size); err != nil {
			t.Fatal(err)
		}
		if size > 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("size_bytes still 1 five seconds after the commit")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebWrites_DeletingAWikiPageIsAllowedAtAFullQuota(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, userStorageQuota)
	r := seedRaceRepo(t, db, reposRoot)
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.WikiPageSave(r.owner.name, r.name, "Home", "hello", raceAuthor, "Add Home"); err != nil {
		t.Fatalf("seed wiki page: %v", err)
	}
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, 5*httpQuotaBytes)

	rr := requestAPI(h, http.MethodDelete, "/api/repos"+r.path+"/wiki/Home", r.owner.token)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("want a delete to go through and redirect, got %d %q", rr.Code, rr.Body.String())
	}
	if _, found, _ := code.WikiPageGet(r.owner.name, r.name, "Home"); found {
		t.Error("the page is still there")
	}
}
