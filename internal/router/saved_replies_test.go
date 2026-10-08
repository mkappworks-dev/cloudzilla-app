package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSavedReplies_RequireSignIn(t *testing.T) {
	h, _, _ := newVerificationRouter(t, config.SMTPConfig{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/user/replies"},
		{http.MethodPost, "/api/user/replies"},
		{http.MethodPatch, "/api/user/replies/1"},
		{http.MethodDelete, "/api/user/replies/1"},
	} {
		if rr := serve(h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
		}
	}
}

func TestSavedReplies_JSONLifecycle(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	suffix := testutil.UniqueSuffix(t)
	title := "greeting " + suffix

	rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/user/replies", me.session, `{"title":"`+title+`","body":"  hello  "}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: got %d %s, want 201", rr.Code, rr.Body)
	}
	var id int64
	var storedBody string
	if err := e.db.QueryRow(`SELECT id, body FROM saved_replies WHERE user_id = $1 AND title = $2`, me.id, title).Scan(&id, &storedBody); err != nil {
		t.Fatalf("reply not stored: %v", err)
	}
	if storedBody != "hello" {
		t.Errorf("stored body = %q, want it trimmed", storedBody)
	}
	path := "/api/user/replies/" + strconv.FormatInt(id, 10)

	list := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/replies", me.session, ""))
	var listed []model.SavedReply
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != id {
		t.Errorf("list = %d %s, want my one reply", list.Code, list.Body)
	}
	theirs := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/replies", other.session, ""))
	if strings.Contains(theirs.Body.String(), title) {
		t.Error("another user's list shows my reply")
	}

	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, other.session, `{"title":"stolen","body":"x"}`)); rr.Code != http.StatusForbidden {
		t.Errorf("another user's update: got %d, want 403", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, me.session, `{"title":"","body":"x"}`)); rr.Code != http.StatusBadRequest {
		t.Errorf("blank title update: got %d, want 400", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, path, me.session, `{"title":"renamed `+suffix+`","body":"new body"}`)); rr.Code != http.StatusNoContent {
		t.Fatalf("update: got %d %s, want 204", rr.Code, rr.Body)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM saved_replies WHERE id = $1 AND title = $2 AND body = 'new body'`, id, "renamed "+suffix); n != 1 {
		t.Error("update did not change the stored reply")
	}

	serve(e.h, importJSONRequest(http.MethodDelete, path, other.session, ""))
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM saved_replies WHERE id = $1`, id); n != 1 {
		t.Fatal("another user deleted my reply")
	}
	if rr := serve(e.h, importJSONRequest(http.MethodDelete, path, me.session, "")); rr.Code != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM saved_replies WHERE id = $1`, id); n != 0 {
		t.Error("delete left the reply behind")
	}
}

func TestSavedReplies_RejectBadInput(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)

	for name, body := range map[string]string{
		"malformed json": `{`,
		"blank title":    `{"title":"  ","body":"x"}`,
		"blank body":     `{"title":"t","body":""}`,
	} {
		if rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/user/replies", me.session, body)); rr.Code != http.StatusBadRequest {
			t.Errorf("create with %s: got %d, want 400", name, rr.Code)
		}
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, "/api/user/replies/1", me.session, `{`)); rr.Code != http.StatusBadRequest {
		t.Errorf("update with malformed json: got %d, want 400", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodPatch, "/api/user/replies/abc", me.session, `{"title":"t","body":"b"}`)); rr.Code != http.StatusBadRequest {
		t.Errorf("update with a non-numeric id: got %d, want 400", rr.Code)
	}
	if rr := serve(e.h, importJSONRequest(http.MethodDelete, "/api/user/replies/abc", me.session, "")); rr.Code != http.StatusBadRequest {
		t.Errorf("delete with a non-numeric id: got %d, want 400", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM saved_replies WHERE user_id = $1`, me.id); n != 0 {
		t.Errorf("rejected requests stored %d replies", n)
	}
}

func TestSavedReplies_HTMXReturnsFragments(t *testing.T) {
	e := newTransferEnv(t)
	me := e.account(t)
	suffix := testutil.UniqueSuffix(t)
	title := "htmx reply " + suffix

	rr := serve(e.h, htmxRequest(browserRequest(http.MethodPost, "/api/user/replies", me.session, url.Values{"title": {title}, "body": {"form body"}})))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `id="saved-replies-list"`) || !strings.Contains(rr.Body.String(), title) {
		t.Fatalf("create: got %d %s, want the list fragment containing the reply", rr.Code, rr.Body)
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM saved_replies WHERE user_id = $1`, me.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	path := "/api/user/replies/" + strconv.FormatInt(id, 10)

	if rr := serve(e.h, htmxRequest(browserRequest(http.MethodPost, "/api/user/replies", me.session, url.Values{"title": {""}, "body": {"b"}}))); rr.Code != http.StatusBadRequest {
		t.Errorf("blank title: got %d, want 400", rr.Code)
	}

	picker := serve(e.h, htmxRequest(browserRequest(http.MethodGet, "/api/user/replies", me.session, nil)))
	if picker.Code != http.StatusOK || !strings.Contains(picker.Body.String(), title) || strings.HasPrefix(strings.TrimSpace(picker.Body.String()), "[") {
		t.Errorf("picker: got %d, want an HTML fragment listing the reply", picker.Code)
	}

	rr = serve(e.h, htmxRequest(browserRequest(http.MethodPatch, path, me.session, url.Values{"title": {"edited " + suffix}, "body": {"edited body"}})))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "edited "+suffix) {
		t.Errorf("update: got %d, want the list fragment with the new title", rr.Code)
	}

	rr = serve(e.h, htmxRequest(browserRequest(http.MethodDelete, path, me.session, nil)))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "edited "+suffix) {
		t.Errorf("delete: got %d, want the list fragment without the reply", rr.Code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM saved_replies WHERE user_id = $1`, me.id); n != 0 {
		t.Errorf("%d replies left after delete", n)
	}
}
