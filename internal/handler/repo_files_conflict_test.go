package handler_test

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (r editRepo) submitEdit(t *testing.T, path string, form url.Values) (int, string) {
	t.Helper()
	rr := send(t, r.api, http.MethodPost, r.owner.token, r.path+"/edit/main/"+path, form, false)
	return rr.Code, html.UnescapeString(rr.Body.String())
}

func TestEditConflict_NonOverlappingChangeMergesIntoTheTextarea(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\nc\nd\ne\n")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "n.txt", "a\nb\nc\nd\nE\n")
	current := r.blobSHA(t, "n.txt")

	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("A\nb\nc\nd\ne\n")}, "message": {"Mine"}, "blob_sha": {opened},
	})
	if status != http.StatusConflict {
		t.Fatalf("want 409, got %d: %.300s", status, body)
	}
	for _, want := range []string{
		">\nA\nb\nc\nd\nE\n</textarea>",
		"were merged into your text",
		"Changed on",
		"@@ -2,4 +2,4 @@",
		`name="blob_sha" value="` + current + `"`,
		`name="upstream_sha" value="` + opened + `"`,
		`value="Mine"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "open the current version") {
		t.Errorf("the old copy-and-reopen text is still shown")
	}

	status, body = r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("A\nb\nc\nd\nE\n")}, "message": {"Mine"}, "blob_sha": {current}, "upstream_sha": {opened},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("resubmit: want 303, got %d: %.300s", status, body)
	}
	if got := r.contents(t, "n.txt"); got != "A\nb\nc\nd\nE\n" {
		t.Errorf("n.txt = %q, want both changes", got)
	}
}

func TestEditConflict_OverlapShowsMarkersAndKeepsTheText(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\nc\n")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "n.txt", "a\ntheirs\nc\n")
	tip := branchHash(t, r.git, "main")

	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("a\nmine\nc\n")}, "message": {"Mine"}, "blob_sha": {opened},
	})
	if status != http.StatusConflict {
		t.Fatalf("want 409, got %d: %.300s", status, body)
	}
	for _, want := range []string{
		"\na\n<<<<<<< your changes\nmine\n=======\ntheirs\n>>>>>>> main (current)\nc\n</textarea>",
		"1 conflict needs resolving",
		"Changed on",
		"@@ -1,3 +1,3 @@",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if got := branchHash(t, r.git, "main"); got != tip {
		t.Errorf("main moved to %s, want it left at %s", got, tip)
	}
}

func TestEditConflict_MarkersStillInTheTextAreRefused(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\nc\n")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "n.txt", "a\ntheirs\nc\n")
	current := r.blobSHA(t, "n.txt")
	tip := branchHash(t, r.git, "main")

	for _, content := range []string{
		"a\n<<<<<<< your changes\nmine\n=======\ntheirs\n>>>>>>> main (current)\nc\n",
		"a\nmine\n>>>>>>> main (current)\nc\n",
	} {
		status, body := r.submitEdit(t, "n.txt", url.Values{
			"path": {"n.txt"}, "content": {crlf(content)}, "blob_sha": {current}, "upstream_sha": {opened},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("want 422, got %d: %.300s", status, body)
		}
		for _, want := range []string{`id="ef-content-error"`, `aria-invalid="true"`, `aria-describedby="ef-content-error"`, "Remove the", "Changed on", content} {
			if !strings.Contains(body, want) {
				t.Errorf("page lacks %q:\n%s", want, body)
			}
		}
		if got := branchHash(t, r.git, "main"); got != tip {
			t.Errorf("main moved to %s, want it left at %s", got, tip)
		}
	}
}

func TestEditConflict_ADividerAloneStillCommits(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.md", "title\nbody\n")
	status, body := r.submitEdit(t, "n.md", url.Values{
		"path": {"n.md"}, "content": {crlf("title\n=======\nbody\n")}, "blob_sha": {r.blobSHA(t, "n.md")},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %.300s", status, body)
	}
	if got := r.contents(t, "n.md"); got != "title\n=======\nbody\n" {
		t.Errorf("n.md = %q", got)
	}
}

func TestEditConflict_DeletedUpstreamKeepsTheTextWithoutAMerge(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\n")
	opened := r.blobSHA(t, "n.txt")
	if _, _, err := r.code.DeleteFile(r.owner.name, r.name, "main", "n.txt", opened, raceAuthor, "Remove n.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("a\nmine\n")}, "message": {"Mine"}, "blob_sha": {opened},
	})
	if status != http.StatusConflict {
		t.Fatalf("want 409, got %d: %.300s", status, body)
	}
	for _, want := range []string{"deleted or moved on main", ">\na\nmine\n</textarea>", "no longer exists on main", `value="Mine"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Changed on") || strings.Contains(body, "<<<<<<<") {
		t.Errorf("a deleted file shows a diff or markers:\n%s", body)
	}
}

func TestEditConflict_CRLFFileKeepsItsLineEndings(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "w.txt", "a\r\nb\r\nc\r\nd\r\ne\r\n")
	opened := r.blobSHA(t, "w.txt")
	r.commitMain(t, "w.txt", "a\r\nb\r\nc\r\nd\r\nE\r\n")
	current := r.blobSHA(t, "w.txt")

	status, body := r.submitEdit(t, "w.txt", url.Values{
		"path": {"w.txt"}, "content": {crlf("A\nb\nc\nd\ne\n")}, "blob_sha": {opened},
	})
	if status != http.StatusConflict || !strings.Contains(body, ">\nA\nb\nc\nd\nE\n</textarea>") {
		t.Fatalf("want 409 with the merged text, got %d: %.400s", status, body)
	}
	status, body = r.submitEdit(t, "w.txt", url.Values{
		"path": {"w.txt"}, "content": {crlf("A\nb\nc\nd\nE\n")}, "blob_sha": {current},
	})
	if status != http.StatusSeeOther {
		t.Fatalf("resubmit: want 303, got %d: %.300s", status, body)
	}
	if got := r.contents(t, "w.txt"); got != "A\r\nb\r\nc\r\nd\r\nE\r\n" {
		t.Errorf("w.txt = %q, want CRLF kept", got)
	}
}

func TestEditConflict_NoTrailingNewlineMerges(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\nc")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "n.txt", "a\nb\nC")
	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("A\nb\nc")}, "blob_sha": {opened},
	})
	if status != http.StatusConflict || !strings.Contains(body, ">\nA\nb\nC</textarea>") {
		t.Fatalf("want 409 with the merged text, got %d: %.400s", status, body)
	}
}

func TestEditConflict_NonTextUpstreamSkipsTheMerge(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\n")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "n.txt", "a\x00b\n")
	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("a\nmine\n")}, "blob_sha": {opened},
	})
	if status != http.StatusConflict {
		t.Fatalf("want 409, got %d: %.300s", status, body)
	}
	for _, want := range []string{"no longer text the editor can open", ">\na\nmine\n</textarea>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Changed on") {
		t.Errorf("a binary upstream shows a diff:\n%s", body)
	}
}

func TestEditConflict_AMovedFileIsReportedAsGone(t *testing.T) {
	r := seedEditRepo(t)
	r.commitMain(t, "n.txt", "a\nb\n")
	opened := r.blobSHA(t, "n.txt")
	r.commitMain(t, "moved.txt", "a\nb\n")
	if _, _, err := r.code.DeleteFile(r.owner.name, r.name, "main", "n.txt", opened, raceAuthor, "Move n.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	status, body := r.submitEdit(t, "n.txt", url.Values{
		"path": {"n.txt"}, "content": {crlf("a\nmine\n")}, "blob_sha": {opened},
	})
	if status != http.StatusConflict || !strings.Contains(body, "deleted or moved") {
		t.Fatalf("want 409 deleted-or-moved, got %d: %.300s", status, body)
	}
}
