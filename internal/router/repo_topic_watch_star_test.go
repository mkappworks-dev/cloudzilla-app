package router_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func topicName(t *testing.T) string {
	return "tp-" + strings.ReplaceAll(testutil.UniqueSuffix(t), "_", "-")
}

func TestTopics_SetRefusals(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/topics")
	stored := func() int {
		return e.count(t, `SELECT COUNT(*) FROM repo_topics WHERE repo_id = $1`, e.repoID)
	}

	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, json: `{"topics":["go"]}`}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.outsider.token, json: `{"topics":["go"]}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.writer.token, json: `{"topics":["go"]}`}), http.StatusForbidden)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: "/api/repos/" + e.owner.name + "/nope/topics", token: e.owner.token, json: `{"topics":["go"]}`}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, json: `{`}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, json: `{"topics":["Bad Name"]}`}), http.StatusBadRequest)
	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = "t" + string(rune('a'+i))
	}
	body, _ := json.Marshal(map[string][]string{"topics": tooMany})
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, json: string(body)}), http.StatusBadRequest)
	if stored() != 0 {
		t.Fatalf("refused requests stored %d topics", stored())
	}
}

func TestTopics_SetListAndClear(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/topics")
	a, b := topicName(t), topicName(t)

	rr := e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, json: `{"topics":["` + a + `","` + b + `"]}`})
	wantStatus(t, rr, http.StatusOK)
	var got []struct{ Name string }
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || len(got) != 2 {
		t.Fatalf("topics = %s (%v)", rr.Body, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM repo_topics WHERE repo_id = $1`, e.repoID); n != 2 {
		t.Errorf("stored %d topics, want 2", n)
	}

	rr = e.do(t, metaReq{method: "GET", target: target})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, a)

	rr = e.do(t, metaReq{method: "GET", target: target, token: e.owner.token, htmx: true})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, b)

	rr = e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, htmx: true, json: `{"topics":["` + a + `"]}`})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, a)
	if strings.Contains(rr.Body.String(), b) {
		t.Error("the replaced topic is still rendered")
	}

	rr = e.do(t, metaReq{method: "PUT", target: target, token: e.owner.token, json: `{}`})
	wantStatus(t, rr, http.StatusOK)
	if n := e.count(t, `SELECT COUNT(*) FROM repo_topics WHERE repo_id = $1`, e.repoID); n != 0 {
		t.Errorf("%d topics left after clearing", n)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("cleared body = %q, want an empty array", got)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/topics"}), http.StatusNotFound)
}

func TestTopicPage(t *testing.T) {
	e := newMetaEnv(t)
	name := topicName(t)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: e.path("/topics"), token: e.owner.token, json: `{"topics":["` + name + `"]}`}), http.StatusOK)

	for _, query := range []string{"", "?sort=updated", "?sort=name&page=2", "?sort=bogus&page=0"} {
		wantStatus(t, e.do(t, metaReq{method: "GET", target: "/topic/" + name + query}), http.StatusOK)
	}
	bodyHas(t, e.do(t, metaReq{method: "GET", target: "/topic/" + name}), e.repoName)

	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/topic/Not%20Valid"}), http.StatusNotFound)
}

func TestWatch_LevelsAndRefusals(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/watch")
	level := func(userID int64) string {
		var l string
		if err := e.db.QueryRow(`SELECT level FROM watches WHERE repo_id = $1 AND user_id = $2`, e.repoID, userID).Scan(&l); err != nil {
			return ""
		}
		return l
	}

	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: "/api/repos/" + e.owner.name + "/nope/watch", token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: "/api/repos/" + e.owner.name + "/nope/watch", token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.outsider.token, form: url.Values{"level": {"shouting"}}}), http.StatusBadRequest)
	if level(e.outsider.id) != "" {
		t.Fatal("refused requests created a watch")
	}

	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.outsider.token}), http.StatusNoContent)
	if got := level(e.outsider.id); got != "watching" {
		t.Errorf("default level = %q, want watching", got)
	}
	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.outsider.token, form: url.Values{"level": {"ignoring"}}}), http.StatusNoContent)
	if got := level(e.outsider.id); got != "ignoring" {
		t.Errorf("level = %q, want ignoring", got)
	}

	wantStatus(t, e.do(t, metaReq{method: "PUT", target: target, token: e.outsider.token, htmx: true, form: url.Values{"level": {"releases_only"}}}), http.StatusOK)
	if got := level(e.outsider.id); got != "releases_only" {
		t.Errorf("level = %q, want releases_only", got)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: target}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: target, token: e.outsider.token}), http.StatusOK)

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusNoContent)
	if level(e.outsider.id) != "" {
		t.Error("unwatch left the watch behind")
	}
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token, htmx: true}), http.StatusOK)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/watch"}), http.StatusNotFound)
}

func TestStar_StarUnstarAndStargazers(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/star")
	starred := func() int {
		return e.count(t, `SELECT COUNT(*) FROM stars WHERE repo_id = $1 AND user_id = $2`, e.repoID, e.outsider.id)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: "/api/repos/" + e.owner.name + "/nope/star", token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: "/api/repos/" + e.owner.name + "/nope/star", token: e.outsider.token}), http.StatusNotFound)
	if starred() != 0 {
		t.Fatal("refused requests starred the repo")
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token}), http.StatusNoContent)
	if starred() != 1 {
		t.Fatal("star not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, htmx: true}), http.StatusOK)
	if starred() != 1 {
		t.Errorf("starring twice stored %d rows", starred())
	}

	rr := e.do(t, metaReq{method: "GET", target: e.path("/stargazers"), htmx: true})
	wantStatus(t, rr, http.StatusOK)
	var users []struct{ Username string }
	if err := json.Unmarshal(rr.Body.Bytes(), &users); err != nil || len(users) != 1 || users[0].Username != e.outsider.name {
		t.Errorf("stargazers = %s (%v)", rr.Body, err)
	}
	if strings.Contains(rr.Body.String(), "@") {
		t.Error("stargazer JSON exposes an email")
	}

	rr = e.do(t, metaReq{method: "GET", target: e.path("/stargazers")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.outsider.name)
	rr = e.do(t, metaReq{method: "GET", target: "/" + e.owner.name + "/" + e.repoName + "/stargazers"})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, e.outsider.name)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/" + e.owner.name + "/nope/stargazers"}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/stargazers"}), http.StatusNotFound)

	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token, htmx: true}), http.StatusOK)
	if starred() != 0 {
		t.Fatal("htmx unstar left the star")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token}), http.StatusNoContent)
	wantStatus(t, e.do(t, metaReq{method: "DELETE", target: target, token: e.outsider.token}), http.StatusNoContent)
	if starred() != 0 {
		t.Error("unstar left the star")
	}
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/stargazers")}), http.StatusOK)
}
