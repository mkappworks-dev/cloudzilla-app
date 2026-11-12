package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestEditPreview_ShowsTheDiff(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "notes.txt", "one\ntwo\nthree\n")
	tip := branchHash(t, r.git, "main")
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit-preview/main/notes.txt",
		url.Values{"path": {"notes.txt"}, "content": {crlf("one\n2\nthree\n")}}, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %.300s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"@@ -1,3 +1,3 @@", "two", ">2<", "+1", "-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Rename") {
		t.Errorf("an unchanged path shows a rename line:\n%s", body)
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Errorf("main = %s, want the preview to leave it at %s", got, tip)
	}
}

func TestEditPreview_ShowsARename(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "notes.txt", "one\n")
	tests := []struct {
		name, content, wantText string
		wantDiff                bool
	}{
		{"rename only", "one\n", "with no change to the contents", false},
		{"rename and edit", "uno\n", "&rarr;", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit-preview/main/notes.txt",
				url.Values{"path": {"docs/renamed.txt"}, "content": {tt.content}}, true)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %.300s", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			for _, want := range []string{tt.wantText, "notes.txt", "docs/renamed.txt"} {
				if !strings.Contains(body, want) {
					t.Errorf("preview lacks %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, "No changes to commit.") {
				t.Errorf("a rename is a change:\n%s", body)
			}
			if got := strings.Contains(body, "@@ -1,1 +1,1 @@"); got != tt.wantDiff {
				t.Errorf("hunk shown = %v, want %v:\n%s", got, tt.wantDiff, body)
			}
		})
	}
}

func TestEditPreview_UnchangedHasNothingToCommit(t *testing.T) {
	r := seedEditRepo(t)
	for name, form := range map[string]url.Values{
		"LF file":   {"path": {"docs/guide.md"}, "content": {crlf("guide\n")}},
		"CRLF file": {"path": {"crlf.txt"}, "content": {crlf("one\ntwo\n")}},
	} {
		t.Run(name, func(t *testing.T) {
			rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit-preview/main/"+form.Get("path"), form, true)
			body := rr.Body.String()
			if rr.Code != http.StatusOK || !strings.Contains(body, "No changes to commit.") || !strings.Contains(body, "Nothing to commit: the path and contents are unchanged.") {
				t.Errorf("want 200 with the no-changes message, got %d: %.300s", rr.Code, body)
			}
		})
	}
}

func TestEditPreview_RefusalsAreJSON(t *testing.T) {
	r := seedEditRepo(t)
	reader := seedSignedInUser(t, r.db)
	tests := []struct {
		name, token, ref, path string
		form                   url.Values
		status                 int
	}{
		{"reader", reader.token, "main", "a.txt", url.Values{"path": {"a.txt"}, "content": {"x"}}, http.StatusForbidden},
		{"missing branch", r.owner.token, "gone", "a.txt", url.Values{"path": {"a.txt"}, "content": {"x"}}, http.StatusNotFound},
		{"tag", r.owner.token, "v1", "a.txt", url.Values{"path": {"a.txt"}, "content": {"x"}}, http.StatusNotFound},
		{"missing file", r.owner.token, "main", "nope.txt", url.Values{"path": {"nope.txt"}, "content": {"x"}}, http.StatusNotFound},
		{"binary file", r.owner.token, "main", "bin.dat", url.Values{"path": {"bin.dat"}, "content": {"x"}}, http.StatusUnprocessableEntity},
		{"symlink", r.owner.token, "main", "link", url.Values{"path": {"link"}, "content": {"x"}}, http.StatusUnprocessableEntity},
		{"invalid new path", r.owner.token, "main", "a.txt", url.Values{"path": {"../x"}, "content": {"x"}}, http.StatusUnprocessableEntity},
		{"content over the cap", r.owner.token, "main", "a.txt", url.Values{"path": {"a.txt"}, "content": {strings.Repeat("a", editCap+1)}}, http.StatusRequestEntityTooLarge},
		{"body over the cap", r.owner.token, "main", "a.txt", url.Values{"path": {"a.txt"}, "content": {strings.Repeat("a", 4<<20)}}, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := send(t, r.api, http.MethodPost, tt.token, r.path+"/edit-preview/"+tt.ref+"/"+tt.path, tt.form, true)
			if rr.Code != tt.status {
				t.Fatalf("want %d, got %d: %.200s", tt.status, rr.Code, rr.Body.String())
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") || len(rr.Body.String()) > 300 {
				t.Errorf("want a short JSON error, got %q with %d bytes", ct, rr.Body.Len())
			}
		})
	}
}

func TestEditPreview_RefusesAnArchivedRepoAndAnonymous(t *testing.T) {
	r := seedEditRepo(t)
	form := url.Values{"path": {"a.txt"}, "content": {"x"}}
	if rr := send(t, r.api, http.MethodPost, "", r.path+"/edit-preview/main/a.txt", form, true); rr.Code < 400 {
		t.Errorf("anonymous: want a refusal, got %d", rr.Code)
	}
	testutil.Exec(t, r.db, `UPDATE repositories SET is_archived = true WHERE id = $1`, r.id)
	if rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit-preview/main/a.txt", form, true); rr.Code != http.StatusForbidden {
		t.Errorf("archived: want 403, got %d", rr.Code)
	}
}

func TestEditPage_HasTheTabsAndTheStayingTextarea(t *testing.T) {
	r := seedEditRepo(t)
	body := send(t, r.api, http.MethodGet, r.owner.token, r.path+"/edit/main/docs/guide.md", nil, false).Body.String()
	for _, want := range []string{
		`role="tablist"`,
		`hx-post="` + r.path + `/edit-preview/main/docs/guide.md"`,
		`id="ef-content"`,
		`id="ef-preview"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}
