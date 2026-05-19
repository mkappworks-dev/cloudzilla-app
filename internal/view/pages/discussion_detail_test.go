package pages_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderReplyFragment(t *testing.T, reply view.RenderedDiscussionReply, count int, canWrite bool) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.DiscussionReplyCreated("acme", "widgets", 2, reply, count, canWrite, true).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// A posted reply must come back as an appendable card plus out-of-band swaps
// that refresh both reply-count spots — that is what makes a new comment land
// in the timeline and the header without a full page reload.
func TestDiscussionReplyCreated_RendersReplyAndOOBCounts(t *testing.T) {
	reply := view.RenderedDiscussionReply{
		DiscussionReply: model.DiscussionReply{ID: 7, AuthorName: "daisy", Body: "looks good", CreatedAt: time.Now()},
		BodyHTML:        "<p>looks good</p>",
	}
	out := renderReplyFragment(t, reply, 5, true)

	for _, want := range []string{
		`id="reply-7"`,
		`id="discussion-reply-count-meta"`,
		`id="discussion-reply-count-heading"`,
		`hx-swap-oob="true"`,
		"5 replies",
		"Mark as answer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fragment missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// A reader without write access sees neither the answer controls nor a Delete
// button (the mockup drops reply deletion entirely).
func TestDiscussionReplyCreated_NoWriteAccessHidesControls(t *testing.T) {
	reply := view.RenderedDiscussionReply{
		DiscussionReply: model.DiscussionReply{ID: 9, AuthorName: "yumi", Body: "hi", CreatedAt: time.Now()},
		BodyHTML:        "<p>hi</p>",
	}
	out := renderReplyFragment(t, reply, 1, false)

	if strings.Contains(out, "Mark as answer") {
		t.Error("non-writer must not see the Mark as answer control")
	}
	if strings.Contains(out, "Delete") {
		t.Error("reply card must not render a Delete button")
	}
	if !strings.Contains(out, "1 reply") {
		t.Errorf("singular count expected\n%s", out)
	}
}
