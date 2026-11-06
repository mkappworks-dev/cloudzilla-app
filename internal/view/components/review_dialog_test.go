package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderReviewDialog(t *testing.T, d ReviewDialogData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ReviewDialog(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestReviewDialog_AuthorCanOnlyComment(t *testing.T) {
	out := renderReviewDialog(t, ReviewDialogData{ReviewsURL: "/api/repos/o/r/pulls/3/reviews", Number: 3, BaseBranch: "main", IsAuthor: true})
	if n := strings.Count(out, "disabled"); n < 2 {
		t.Errorf("want the Approve and Request changes radios disabled, found %d disabled attributes", n)
	}
	if !strings.Contains(out, "you can only comment on it") {
		t.Error("missing the line explaining why the author can only comment")
	}
}

func TestReviewDialog_ReviewerGetsAllThreeVerdicts(t *testing.T) {
	out := renderReviewDialog(t, ReviewDialogData{ReviewsURL: "/api/repos/o/r/pulls/3/reviews", Number: 3, BaseBranch: "main"})
	for _, v := range []string{`value="commented"`, `value="approved"`, `value="changes_requested"`} {
		if !strings.Contains(out, v) {
			t.Errorf("missing radio %s", v)
		}
	}
	if strings.Contains(out, " disabled ") {
		t.Error("a reviewer's radio is disabled")
	}
	if !strings.Contains(out, `hx-post="/api/repos/o/r/pulls/3/reviews"`) || !strings.Contains(out, "location.reload()") {
		t.Error("form must post to the reviews endpoint and reload the page")
	}
	if !strings.Contains(out, `:required="state !== 'approved'"`) {
		t.Errorf("a summary is required for Comment and Request changes only: %s", out)
	}
}
