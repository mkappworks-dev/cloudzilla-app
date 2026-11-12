package main

import (
	"strconv"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

const hour = time.Hour

func fixtureOrg() model.Organization {
	return model.Organization{
		ID: 1, Name: "paper-planes", DisplayName: "Paper Planes", Description: "Small tools for people who run their own servers.",
		Website: "https://paper-planes.example.test", Location: "Lisbon, Portugal", ContactEmail: "hello@paper-planes.example.test",
		DefaultRepoVisibility: "private", DefaultBranchName: "main", CreatedAt: ago(days(400)), UpdatedAt: ago(days(20)),
	}
}

func fixtureMembers() []model.OrgMember {
	return []model.OrgMember{
		{ID: 1, OrgID: 1, UserID: 1, Username: fixtureUser, Role: model.OrgRoleOwner, CreatedAt: ago(days(400))},
		{ID: 2, OrgID: 1, UserID: 2, Username: "ada-silva", Role: model.OrgRoleOwner, CreatedAt: ago(days(380))},
		{ID: 3, OrgID: 1, UserID: 3, Username: "jonas-weber", Role: model.OrgRoleMember, CreatedAt: ago(days(120))},
		{ID: 4, OrgID: 1, UserID: 4, Username: "priya-nair", Role: model.OrgRoleMember, CreatedAt: ago(days(60))},
	}
}

func fixtureUserRow() model.User {
	return model.User{
		ID: 1, Username: fixtureUser, Email: "mk@example.test", Name: "Malith Kuruppu",
		Bio: "Self-hosting enthusiast. Writes Go and runs a rack in the garage.", Company: "Paper Planes", Location: "Colombo, Sri Lanka",
		CreatedAt: ago(days(500)), UpdatedAt: ago(days(2)), EmailNotifications: true, NotifyPRReview: true, NotifyMention: true,
		EmailDigest: "daily",
	}
}

func fixturePinned() []components.PinnedRepoData {
	return []components.PinnedRepoData{
		{OwnerName: fixtureUser, Name: "hollow-atlas", Description: "Offline-first map tiles for self-hosted fleets.", Language: "Go", LanguageColor: "#00ADD8", Stars: 128},
		{OwnerName: fixtureUser, Name: "rusty-meadow", Description: "Static site generator with a tiny runtime.", Language: "Rust", LanguageColor: "#dea584", Stars: 54},
	}
}

func fixtureLangs() []components.LangBarItem {
	return []components.LangBarItem{
		{Name: "Go", Percent: 62, Color: "#00ADD8"}, {Name: "Rust", Percent: 24, Color: "#dea584"}, {Name: "TypeScript", Percent: 14, Color: "#3178c6"},
	}
}

func fixtureHeat() map[time.Time]int {
	heat := map[time.Time]int{}
	for i := 0; i < 120; i++ {
		heat[time.Date(fixtureYear, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i*2)] = i%5 + 1
	}
	return heat
}

func fixtureEvents() []model.Event {
	return []model.Event{
		{ID: 1, ActorName: fixtureUser, RepoName: "hollow-atlas", OwnerName: fixtureUser, EventType: "push", Payload: []byte(`{"branch":"main","commit_total":3,"commits":[{"sha":"a1b2c3d","message":"Cache the tile index"}]}`), CreatedAt: ago(days(9))},
		{ID: 2, ActorName: fixtureUser, RepoName: "rusty-meadow", OwnerName: fixtureUser, EventType: "push", Payload: []byte(`{"branch":"main","commit_total":1,"commits":[{"sha":"e4f5a6b","message":"Stream large files"}]}`), CreatedAt: ago(days(12))},
	}
}

func userData(b view.BasePage) view.UserData {
	repos := fixtureRepos()
	return view.UserData{
		BasePage: b, User: fixtureUserRow(), Repos: repos, RecentActivity: fixtureEvents(), IsOwnProfile: true, Tab: "overview",
		PinnedRepos: fixturePinned(), Heatmap: fixtureHeat(), TopLangs: fixtureLangs(),
		Orgs:       []service.OrgMembership{{Org: fixtureOrg(), Role: model.OrgRoleOwner}},
		ReposTotal: len(repos), StarsTotal: 12, GistsTotal: 3, HasProfileRepo: false,
	}
}

func orgData(b view.BasePage) view.OrgData {
	owner := model.OrgRoleOwner
	joined := ago(days(400))
	return view.OrgData{
		BasePage: b, Org: fixtureOrg(), Repos: fixtureRepos(), Members: fixtureMembers(), MemberCount: 4, CanManage: true,
		PinnedRepos: orgRepos(), RecentRepos: orgRepos(), TopLangs: fixtureLangs(), ViewerRole: &owner, ViewerJoined: &joined,
	}
}

func attentionItems() []service.AttentionItem {
	return []service.AttentionItem{
		{Kind: service.AttentionPRReviewRequested, RefID: 41, RepoName: fixtureUser + "/hollow-atlas", Title: "Cache tile index on disk", Number: 41,
			UpdatedAt: ago(days(10)), WaitingSince: ago(days(10)), URL: "/" + fixtureUser + "/hollow-atlas/pulls/41", Actor: "ada-silva"},
		{Kind: service.AttentionPRReviewRequested, RefID: 7, RepoName: fixtureUser + "/rusty-meadow", Title: "Stream large files instead of buffering", Number: 7,
			UpdatedAt: ago(days(13)), WaitingSince: ago(days(13)), URL: "/" + fixtureUser + "/rusty-meadow/pulls/7", Actor: "jonas-weber"},
	}
}

func fixturePullItems() []store.PullListItem {
	return []store.PullListItem{
		{ID: 1, Number: 41, Title: "Cache tile index on disk", State: "open", AuthorID: 2, RepoFullName: fixtureUser + "/hollow-atlas", UpdatedAt: ago(days(10)), CreatedAt: ago(days(14))},
		{ID: 2, Number: 7, Title: "Stream large files instead of buffering", State: "open", AuthorID: 3, RepoFullName: fixtureUser + "/rusty-meadow", UpdatedAt: ago(days(13)), CreatedAt: ago(days(20))},
		{ID: 3, Number: 3, Title: "Draft: rework the shipper queue", State: "open", IsDraft: true, AuthorID: 1, RepoFullName: fixtureUser + "/paper-trail", UpdatedAt: ago(days(30)), CreatedAt: ago(days(32))},
	}
}

func fixtureIssueItems() []store.IssueListItem {
	return []store.IssueListItem{
		{ID: 1, Number: 18, Title: "Tile index grows without bound", State: "open", AuthorID: 2, AuthorName: "ada-silva", Visibility: "public", RepoFullName: fixtureUser + "/hollow-atlas", UpdatedAt: ago(days(9)), CreatedAt: ago(days(25))},
		{ID: 2, Number: 5, Title: "Document the on-call handoff", State: "open", AuthorID: 3, AuthorName: "jonas-weber", Visibility: "private", RepoFullName: fixtureUser + "/quiet-harbor", UpdatedAt: ago(days(20)), CreatedAt: ago(days(40))},
	}
}

func fixtureNotifications() []model.NotificationGroup {
	n := func(id int64, t model.NotificationType, actor, repo, title string, num int, kind string, read bool, age time.Duration) model.NotificationGroup {
		return model.NotificationGroup{
			Notification: model.Notification{
				ID: id, UserID: 1, ActorName: actor, Type: t, RepoName: repo, OwnerName: fixtureUser, SubjectID: id,
				SubjectURL: "/" + fixtureUser + "/" + repo + "/" + kind + "/" + strconv.Itoa(num), SubjectTitle: title, SubjectKind: kind, Read: read, CreatedAt: ago(age),
			},
			Count: 1, Unread: map[bool]int{true: 0, false: 1}[read],
		}
	}
	return []model.NotificationGroup{
		n(1, model.NotifPRReview, "ada-silva", "hollow-atlas", "Cache tile index on disk", 41, "pulls", false, days(10)),
		n(2, model.NotifIssueComment, "jonas-weber", "quiet-harbor", "Document the on-call handoff", 5, "issues", false, days(14)),
		n(3, model.NotifPRMerged, "priya-nair", "rusty-meadow", "Stream large files instead of buffering", 7, "pulls", true, days(20)),
	}
}

func orgRepos() []components.PinnedRepoData {
	return []components.PinnedRepoData{
		{OwnerName: "paper-planes", Name: "ledger", Description: "Double-entry bookkeeping for small co-ops.", Language: "Go", LanguageColor: "#00ADD8", Stars: 212},
		{OwnerName: "paper-planes", Name: "signal-fence", Description: "Rate limiting sidecar with a tiny footprint.", Language: "Rust", LanguageColor: "#dea584", Stars: 87},
	}
}
