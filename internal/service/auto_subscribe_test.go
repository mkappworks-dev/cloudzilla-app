package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type autoSubscriber interface {
	AutoSubscribe(ctx context.Context, userID, repoID int64, kind string, number int64, reason string) error
}

type failingSubscriber struct{}

func (failingSubscriber) AutoSubscribe(context.Context, int64, int64, string, int64, string) error {
	return errors.New("subscription store down")
}

type autoSubEnv struct {
	ownerName, repoName  string
	repoID               int64
	author, other, third int64
	otherName, thirdName string

	threads    *service.ThreadSubscriptionService
	issues     *service.IssueService
	pulls      *service.PullService
	discussion *service.DiscussionService
	comments   *service.CommentService
	reviews    *service.PullReviewService
	assignees  *service.AssigneeService

	// Threads opened by author before the action under test.
	issueN, pullN, discussionN int
	issueID, pullID            int64
	seededDiscussion           model.Discussion
	categoryID                 int64
}

// newAutoSubEnv wires every participation service to sub, or to the real
// thread service when sub is nil.
func newAutoSubEnv(t *testing.T, sub autoSubscriber) *autoSubEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	e := &autoSubEnv{ownerName: "testuser_" + suffix, repoName: "testrepo_" + suffix}
	e.author = testutil.SeedUser(t, db, suffix)
	e.other = testutil.SeedUser(t, db, "other_"+suffix)
	e.third = testutil.SeedUser(t, db, "third_"+suffix)
	e.otherName, e.thirdName = "testuser_other_"+suffix, "testuser_third_"+suffix
	e.repoID = testutil.SeedRepo(t, db, e.author, e.ownerName, suffix)

	watches := store.NewWatchStore(db)
	e.threads = service.NewThreadSubscriptionService(store.NewThreadSubscriptionStore(db), watches)
	if sub == nil {
		sub = e.threads
	}
	repoStore := store.NewRepoStore(db)
	userStore := store.NewUserStore(db)
	repoSvc := service.NewRepoService(repoStore, userStore, store.NewOrgStore(db), nil, nil, config.GitConfig{})
	userSvc := service.NewUserService(userStore, config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	notifSvc := service.NewNotificationService(store.NewNotificationStore(db), watches, repoSvc, service.NewEmailService(config.SMTPConfig{}), userSvc)

	e.issues = service.NewIssueService(store.NewIssueStore(db), repoStore, store.NewPullStore(db), repoSvc).WithThreadSubscriptions(sub)
	e.pulls = service.NewPullService(store.NewPullStore(db), repoStore, repoSvc).WithThreadSubscriptions(sub)
	e.discussion = service.NewDiscussionService(store.NewDiscussionStore(db), repoStore).WithThreadSubscriptions(sub)
	e.comments = service.NewCommentService(store.NewCommentStore(db), store.NewMentionStore(db), userSvc, notifSvc, repoSvc).WithThreadSubscriptions(sub)
	e.reviews = service.NewPullReviewService(store.NewPullReviewStore(db), store.NewPullStore(db), repoStore, nil).WithThreadSubscriptions(sub)
	e.assignees = service.NewAssigneeService(store.NewAssigneeStore(db), repoStore, store.NewIssueStore(db), store.NewPullStore(db), userStore).WithThreadSubscriptions(sub)

	ctx := context.Background()
	cats, err := e.discussion.ListCategories(ctx)
	if err != nil || len(cats) == 0 {
		t.Fatalf("discussion categories: %v (%d)", err, len(cats))
	}
	e.categoryID = cats[0].ID

	issue, err := e.issues.Create(ctx, e.ownerName, e.repoName, e.author, "an issue", "", "public")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	e.issueN, e.issueID = issue.Number, issue.ID
	pull, err := e.pulls.Create(ctx, e.ownerName, e.repoName, e.author, "a pull", "", "feature", "main", false, nil)
	if err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	e.pullN, e.pullID = pull.Number, pull.ID
	d, err := e.discussion.Create(ctx, e.ownerName, e.repoName, e.author, e.ownerName, e.categoryID, "a discussion", "")
	if err != nil {
		t.Fatalf("seed discussion: %v", err)
	}
	e.discussionN, e.seededDiscussion = d.Number, *d
	return e
}

func (e *autoSubEnv) repo() model.Repository {
	return model.Repository{ID: e.repoID, OwnerName: e.ownerName, Name: e.repoName}
}

// action is one participation. run returns the number of the thread it touched.
type action struct {
	name   string
	user   func(*autoSubEnv) int64
	kind   string
	reason string
	run    func(ctx context.Context, e *autoSubEnv) (int, error)
}

func actions() []action {
	author := func(e *autoSubEnv) int64 { return e.author }
	other := func(e *autoSubEnv) int64 { return e.other }
	third := func(e *autoSubEnv) int64 { return e.third }
	return []action{
		{"create issue", author, model.ThreadKindIssue, model.ThreadReasonAuthor, func(ctx context.Context, e *autoSubEnv) (int, error) {
			i, err := e.issues.Create(ctx, e.ownerName, e.repoName, e.author, "new issue", "", "public")
			if err != nil {
				return 0, err
			}
			return i.Number, nil
		}},
		{"create pull", author, model.ThreadKindPull, model.ThreadReasonAuthor, func(ctx context.Context, e *autoSubEnv) (int, error) {
			p, err := e.pulls.Create(ctx, e.ownerName, e.repoName, e.author, "new pull", "", "feature-2", "main", false, nil)
			if err != nil {
				return 0, err
			}
			return p.Number, nil
		}},
		{"create discussion", author, model.ThreadKindDiscussion, model.ThreadReasonAuthor, func(ctx context.Context, e *autoSubEnv) (int, error) {
			d, err := e.discussion.Create(ctx, e.ownerName, e.repoName, e.author, e.ownerName, e.categoryID, "new discussion", "")
			if err != nil {
				return 0, err
			}
			return d.Number, nil
		}},
		{"comment on issue", other, model.ThreadKindIssue, model.ThreadReasonComment, func(ctx context.Context, e *autoSubEnv) (int, error) {
			_, err := e.comments.CreateForIssue(ctx, e.repo(), e.issueID, e.issueN, e.other, e.otherName, "hello")
			return e.issueN, err
		}},
		{"comment on pull", other, model.ThreadKindPull, model.ThreadReasonComment, func(ctx context.Context, e *autoSubEnv) (int, error) {
			_, err := e.comments.CreateForPull(ctx, e.repo(), e.pullID, e.pullN, e.other, e.otherName, "hello")
			return e.pullN, err
		}},
		{"reply to discussion", other, model.ThreadKindDiscussion, model.ThreadReasonComment, func(ctx context.Context, e *autoSubEnv) (int, error) {
			_, err := e.discussion.CreateReply(ctx, e.seededDiscussion, e.other, e.otherName, "hello", nil)
			return e.discussionN, err
		}},
		{"review pull", other, model.ThreadKindPull, model.ThreadReasonReview, func(ctx context.Context, e *autoSubEnv) (int, error) {
			_, err := e.reviews.SubmitReview(ctx, e.ownerName, e.repoName, e.pullN, e.other, e.otherName, "approved", "LGTM")
			return e.pullN, err
		}},
		{"assign to issue", third, model.ThreadKindIssue, model.ThreadReasonAssign, func(ctx context.Context, e *autoSubEnv) (int, error) {
			return e.issueN, e.assignees.AddToIssue(ctx, e.ownerName, e.repoName, e.issueN, e.thirdName)
		}},
		{"assign to pull", third, model.ThreadKindPull, model.ThreadReasonAssign, func(ctx context.Context, e *autoSubEnv) (int, error) {
			return e.pullN, e.assignees.AddToPull(ctx, e.ownerName, e.repoName, e.pullN, e.thirdName)
		}},
	}
}

func (e *autoSubEnv) status(t *testing.T, userID int64, kind string, number int) service.ThreadStatus {
	t.Helper()
	st, err := e.threads.Status(context.Background(), userID, e.repoID, kind, int64(number))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return st
}

func TestAutoSubscribe_EachActionSubscribesTheRightUser(t *testing.T) {
	for _, a := range actions() {
		t.Run(a.name, func(t *testing.T) {
			e := newAutoSubEnv(t, nil)
			number, err := a.run(context.Background(), e)
			if err != nil {
				t.Fatalf("%s: %v", a.name, err)
			}
			st := e.status(t, a.user(e), a.kind, number)
			if st.State != model.ThreadStateSubscribed || st.Reason != a.reason {
				t.Errorf("status = %+v, want subscribed/%s", st, a.reason)
			}
		})
	}
}

func TestAutoSubscribe_AssigneeIsSubscribedNotOtherUsers(t *testing.T) {
	e := newAutoSubEnv(t, nil)
	if err := e.assignees.AddToIssue(context.Background(), e.ownerName, e.repoName, e.issueN, e.thirdName); err != nil {
		t.Fatalf("AddToIssue: %v", err)
	}
	if st := e.status(t, e.other, model.ThreadKindIssue, e.issueN); st.State != "" {
		t.Errorf("a user who wasn't assigned has status %+v, want none", st)
	}
}

func TestAutoSubscribe_FirstReasonIsKept(t *testing.T) {
	e := newAutoSubEnv(t, nil)
	ctx := context.Background()
	if _, err := e.comments.CreateForIssue(ctx, e.repo(), e.issueID, e.issueN, e.author, "author", "again"); err != nil {
		t.Fatalf("comment: %v", err)
	}

	st := e.status(t, e.author, model.ThreadKindIssue, e.issueN)
	if st.Reason != model.ThreadReasonAuthor {
		t.Errorf("reason after the author commented = %q, want %q", st.Reason, model.ThreadReasonAuthor)
	}
}

func TestAutoSubscribe_MutedThreadStaysMuted(t *testing.T) {
	for _, a := range actions() {
		if a.name == "create issue" || a.name == "create pull" || a.name == "create discussion" {
			continue // a new thread has no earlier mute
		}
		t.Run(a.name, func(t *testing.T) {
			e := newAutoSubEnv(t, nil)
			ctx := context.Background()
			muteFor := map[string]int{model.ThreadKindIssue: e.issueN, model.ThreadKindPull: e.pullN, model.ThreadKindDiscussion: e.discussionN}
			if err := e.threads.Set(ctx, a.user(e), e.repoID, a.kind, int64(muteFor[a.kind]), model.ThreadStateMuted); err != nil {
				t.Fatalf("mute: %v", err)
			}

			if _, err := a.run(ctx, e); err != nil {
				t.Fatalf("%s: %v", a.name, err)
			}

			if st := e.status(t, a.user(e), a.kind, muteFor[a.kind]); st.State != model.ThreadStateMuted {
				t.Errorf("status = %+v, want muted", st)
			}
		})
	}
}

func TestAutoSubscribe_WriteFailureDoesNotFailTheRequest(t *testing.T) {
	for _, a := range actions() {
		t.Run(a.name, func(t *testing.T) {
			e := newAutoSubEnv(t, failingSubscriber{})
			if _, err := a.run(context.Background(), e); err != nil {
				t.Errorf("%s failed because the subscription write did: %v", a.name, err)
			}
		})
	}
}
