package router_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestReactions_List(t *testing.T) {
	e := newMetaEnv(t)
	testutil.Exec(t, e.db, `INSERT INTO reactions (user_id, comment_id, emoji) VALUES ($1, $2, 'rocket')`, e.owner.id, e.commentID)

	rr := e.do(t, metaReq{method: "GET", target: e.path("/comments/%d/reactions", e.commentID)})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "rocket")

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/comments/x/reactions")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/comments/999999999/reactions")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: "/api/repos/" + e.owner.name + "/nope/comments/1/reactions"}), http.StatusNotFound)

	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/comments/%d/reactions", e.commentID), token: e.outsider.token}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/comments/%d/reactions", e.commentID), token: e.writer.token}), http.StatusOK)
}

func TestReactions_CommentOfAnotherRepoIsNotFound(t *testing.T) {
	e := newMetaEnv(t)
	other := testutil.SeedRepo(t, e.db, e.owner.id, e.owner.name, testutil.UniqueSuffix(t))
	var issueID, foreign int64
	if err := e.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'i') RETURNING id`, other, e.owner.id).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, 'x') RETURNING id`, other, issueID, e.owner.id).Scan(&foreign); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, e.do(t, metaReq{method: "GET", target: e.path("/comments/%d/reactions", foreign)}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/comments/%d/reactions", foreign), token: e.owner.token, form: url.Values{"emoji": {"heart"}}}), http.StatusNotFound)
	if n := e.count(t, `SELECT COUNT(*) FROM reactions WHERE comment_id = $1`, foreign); n != 0 {
		t.Errorf("reaction landed on a comment outside the URL's repo: %d", n)
	}
}

func TestReactions_Toggle(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/comments/%d/reactions", e.commentID)
	mine := func(emoji string) int {
		return e.count(t, `SELECT COUNT(*) FROM reactions WHERE comment_id = $1 AND user_id = $2 AND emoji = $3`, e.commentID, e.outsider.id, emoji)
	}
	form := func(emoji string) url.Values { return url.Values{"emoji": {emoji}} }

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: form("heart")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/comments/x/reactions"), token: e.outsider.token, form: form("heart")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/comments/999999999/reactions"), token: e.outsider.token, form: form("heart")}), http.StatusNotFound)
	rr := e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form("thumbsup")})
	wantStatus(t, rr, http.StatusBadRequest)
	bodyHas(t, rr, "unsupported emoji")

	rr = e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form("heart")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "heart")
	if mine("heart") != 1 {
		t.Fatal("reaction not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form("heart")}), http.StatusOK)
	if mine("heart") != 0 {
		t.Error("second toggle did not remove the reaction")
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form("heart")}), http.StatusNotFound)
}

func TestDiscussionReactions_Toggle(t *testing.T) {
	e := newMetaEnv(t)
	target := e.path("/discussions/1/reactions")
	mine := func() int {
		return e.count(t, `SELECT COUNT(*) FROM reactions WHERE discussion_id = $1 AND user_id = $2`, e.discussionID, e.writer.id)
	}
	form := func(emoji string) url.Values { return url.Values{"emoji": {emoji}} }

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: form("eyes")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/x/reactions"), token: e.writer.token, form: form("eyes")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/9/reactions"), token: e.writer.token, form: form("eyes")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("nope")}), http.StatusBadRequest)

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("eyes")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "reactions-discussion-1")
	if mine() != 1 {
		t.Fatal("reaction not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("eyes")}), http.StatusOK)
	if mine() != 0 {
		t.Error("second toggle did not remove the reaction")
	}

	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, e.repoID)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.outsider.token, form: form("eyes")}), http.StatusNotFound)
}

func TestDiscussionReplyReactions_Toggle(t *testing.T) {
	e := newMetaEnv(t)
	var replyID, foreignReply int64
	if err := e.db.QueryRow(`INSERT INTO discussion_replies (discussion_id, author_id, author_name, body) VALUES ($1, $2, $3, 'r') RETURNING id`, e.discussionID, e.owner.id, e.owner.name).Scan(&replyID); err != nil {
		t.Fatal(err)
	}
	var otherDiscussion int64
	if err := e.db.QueryRow(`INSERT INTO discussions (repo_id, category_id, number, title, author_id, author_name) VALUES ($1, (SELECT MIN(id) FROM discussion_categories), 2, 'other', $2, $3) RETURNING id`, e.repoID, e.owner.id, e.owner.name).Scan(&otherDiscussion); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`INSERT INTO discussion_replies (discussion_id, author_id, author_name, body) VALUES ($1, $2, $3, 'r') RETURNING id`, otherDiscussion, e.owner.id, e.owner.name).Scan(&foreignReply); err != nil {
		t.Fatal(err)
	}
	target := e.path("/discussions/1/replies/%d/reactions", replyID)
	form := func(emoji string) url.Values { return url.Values{"emoji": {emoji}} }
	mine := func() int {
		return e.count(t, `SELECT COUNT(*) FROM reactions WHERE discussion_reply_id =$1 AND user_id = $2`, replyID, e.writer.id)
	}

	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, form: form("heart")}), http.StatusUnauthorized)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/x/replies/%d/reactions", replyID), token: e.writer.token, form: form("heart")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/1/replies/x/reactions"), token: e.writer.token, form: form("heart")}), http.StatusBadRequest)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/9/replies/%d/reactions", replyID), token: e.writer.token, form: form("heart")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: e.path("/discussions/1/replies/%d/reactions", foreignReply), token: e.writer.token, form: form("heart")}), http.StatusNotFound)
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("nope")}), http.StatusBadRequest)

	rr := e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("heart")})
	wantStatus(t, rr, http.StatusOK)
	bodyHas(t, rr, "reactions-reply-")
	if mine() != 1 {
		t.Fatal("reaction not stored")
	}
	wantStatus(t, e.do(t, metaReq{method: "POST", target: target, token: e.writer.token, form: form("heart")}), http.StatusOK)
	if mine() != 0 {
		t.Error("second toggle did not remove the reaction")
	}
}
