package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func threadSub(segment string, state, reason string) view.ThreadSubscriptionData {
	return view.ThreadSubscriptionData{Owner: "acme", RepoName: "widgets", Segment: segment, Number: 3, State: state, Reason: reason}
}

type threadPage struct {
	name    string
	segment string
	render  func(t *testing.T, sub view.ThreadSubscriptionData, loggedIn bool) string
}

func threadPages() []threadPage {
	base := func(loggedIn bool) view.BasePage {
		if !loggedIn {
			return view.BasePage{}
		}
		return view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "daisy"}}
	}
	return []threadPage{
		{"issue", "issues", func(t *testing.T, sub view.ThreadSubscriptionData, loggedIn bool) string {
			return renderToString(t, pages.IssueDetail(view.IssueDetailData{
				BasePage: base(loggedIn), Owner: "acme", RepoName: "widgets",
				Issue: model.Issue{Number: 3, Title: "Crash"}, ThreadSubscription: sub,
			}))
		}},
		{"pull", "pulls", func(t *testing.T, sub view.ThreadSubscriptionData, loggedIn bool) string {
			return renderToString(t, pages.PullDetail(view.PullDetailData{
				BasePage: base(loggedIn), Owner: "acme", RepoName: "widgets",
				Pull: model.PullRequest{Number: 3, Title: "Retry"}, ThreadSubscription: sub,
			}))
		}},
		{"discussion", "discussions", func(t *testing.T, sub view.ThreadSubscriptionData, loggedIn bool) string {
			return renderToString(t, pages.DiscussionDetail(view.DiscussionDetailData{
				BasePage: base(loggedIn), Owner: "acme", RepoName: "widgets",
				Discussion: model.Discussion{Number: 3, Title: "SSH certs"}, ThreadSubscription: sub,
			}))
		}},
	}
}

func TestThreadSubscription_PagesRenderEveryState(t *testing.T) {
	states := []struct {
		name    string
		state   string
		reason  string
		want    []string
		notWant []string
	}{
		{"not subscribed", "", "", []string{`aria-pressed="false"`, "Subscribe", `{&#34;state&#34;:&#34;subscribed&#34;}`, "You’re not receiving notifications from this thread."}, []string{"Unsubscribe", "Muted"}},
		{"commented", model.ThreadStateSubscribed, model.ThreadReasonComment, []string{`aria-pressed="true"`, "Unsubscribe", `{&#34;state&#34;:&#34;muted&#34;}`, "You’re receiving notifications.", "You commented."}, []string{"Muted"}},
		{"watching repo", model.ThreadStateSubscribed, model.ThreadReasonWatching, []string{"Unsubscribe", "You’re watching this repository."}, []string{"Muted"}},
		{"muted", model.ThreadStateMuted, model.ThreadReasonManual, []string{"Muted", `aria-pressed="false"`, "Subscribe", "You muted this thread. You won’t be notified unless someone <em>@-mentions you</em>."}, []string{"Unsubscribe"}},
	}
	for _, p := range threadPages() {
		for _, s := range states {
			t.Run(p.name+"/"+s.name, func(t *testing.T) {
				out := p.render(t, threadSub(p.segment, s.state, s.reason), true)
				endpoint := `hx-put="/api/repos/acme/widgets/` + p.segment + `/3/subscription"`
				for _, want := range append(s.want, endpoint, `id="thread-subscription"`) {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q", want)
					}
				}
				for _, bad := range s.notWant {
					if strings.Contains(out, bad) {
						t.Errorf("unexpected %q", bad)
					}
				}
			})
		}
	}
}

func TestThreadSubscription_HiddenFromAnonymousViewers(t *testing.T) {
	for _, p := range threadPages() {
		t.Run(p.name, func(t *testing.T) {
			if out := p.render(t, threadSub(p.segment, "", ""), false); strings.Contains(out, "thread-subscription") {
				t.Error("anonymous viewer must not see the control")
			}
		})
	}
}

func TestPullDetail_NoLongerOffersRepoWatchSubscribe(t *testing.T) {
	out := threadPages()[1].render(t, threadSub("pulls", "", ""), true)
	if strings.Contains(out, "/watch") {
		t.Error("the PR sidebar must not touch the repo watch")
	}
}

func TestThreadSubscriptionFragment_RendersSection(t *testing.T) {
	out := renderToString(t, fragments.ThreadSubscription(threadSub("issues", model.ThreadStateMuted, model.ThreadReasonManual)))
	if !strings.Contains(out, "Muted") || !strings.Contains(out, `id="thread-subscription"`) {
		t.Errorf("fragment output unexpected: %s", out)
	}
}
