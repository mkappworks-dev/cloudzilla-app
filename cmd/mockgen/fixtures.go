package main

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// Dates sit more than a week before any run: the view helpers print "3 days ago" off time.Now, which would change
// the output between runs, and fall back to an absolute date past a week.
var (
	fixedNow    = time.Date(2025, time.June, 12, 9, 30, 0, 0, time.UTC)
	fixtureYear = 2025
)

const fixtureUser = "mkappworks"

func ago(d time.Duration) time.Time { return fixedNow.Add(-d) }

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// baseFor builds the chrome for a viewer; the handler's basePage needs services and a request, so it is rebuilt here.
func baseFor(v variant) view.BasePage {
	b := view.BasePage{AllowLogin: true, AllowRegistration: true}
	if v.Viewer == Anon {
		return b
	}
	b.CurrentUser = &middleware.Claims{UserID: 1, Username: fixtureUser, IsSuperadmin: v.Viewer == Admin}
	b.UnreadNotifCount = 3
	b.UserOrgs = []view.OrgEntry{{
		Org:  model.Organization{ID: 1, Name: "paper-planes", DisplayName: "Paper Planes"},
		Role: model.OrgRoleOwner,
	}}
	if v.AdminMode {
		b.AdminMode = &view.AdminModeInfo{ExpiresAt: fixedNow.Add(30 * time.Minute)}
	}
	return b
}

// onAccountPage adds the account subnav the signed-in account pages share; error pages such as the 404 have none.
func onAccountPage(b view.BasePage, active string) view.BasePage {
	if b.CurrentUser != nil {
		b.AccountSubnav = &view.AccountSubnavInfo{Active: active, Counts: map[string]int{
			"repositories": 6, "pulls": 4, "issues": 7, "attention": 3,
		}}
	}
	return b
}

func fixtureRepos() []model.Repository {
	mk := func(id int64, name, desc, lang string, private bool, updated time.Duration) model.Repository {
		l := lang
		return model.Repository{
			ID: id, OwnerID: 1, OwnerName: fixtureUser, Name: name, Description: desc, Private: private,
			DefaultBranch: "main", AllowIssues: true, AllowWiki: true, AllowProjects: true, AllowDiscussions: true,
			CreatedAt: ago(days(200)), UpdatedAt: ago(updated), PrimaryLanguage: &l,
		}
	}
	return []model.Repository{
		mk(1, "hollow-atlas", "Offline-first map tiles for self-hosted fleets.", "Go", false, days(9)),
		mk(2, "rusty-meadow", "Static site generator with a tiny runtime.", "Rust", false, days(12)),
		mk(3, "quiet-harbor", "Internal runbooks and on-call notes.", "Markdown", true, days(20)),
		mk(4, "paper-trail", "Audit log shipper.", "TypeScript", true, days(34)),
	}
}

func homeData(b view.BasePage) view.HomeData {
	repos := fixtureRepos()
	b = onAccountPage(b, "overview")
	heat := map[time.Time]int{}
	for i := 0; i < 120; i++ {
		d := time.Date(fixtureYear, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i*2)
		heat[d] = i%5 + 1
	}
	return view.HomeData{
		BasePage:     b,
		Repos:        repos,
		TotalRepos:   len(repos),
		RepoOpenPRs:  map[int64]int{1: 2, 2: 1},
		RepoSort:     "updated",
		RepoFilter:   "all",
		Stats:        []components.StatItem{{Label: "Repositories", Value: 4, Detail: "2 private, 2 public"}, {Label: "Pull requests", Value: 3, Subtitle: "open"}, {Label: "Issues", Value: 7, Subtitle: "open"}},
		Heatmap:      heat,
		HeatmapYear:  fixtureYear,
		HeatmapTotal: 312,
		HeatmapYears: []int{fixtureYear, fixtureYear - 1},
		Attention: []service.AttentionItem{{
			Kind: service.AttentionPRReviewRequested, RepoName: fixtureUser + "/hollow-atlas", Title: "Cache tile index on disk",
			Number: 41, UpdatedAt: ago(days(10)), WaitingSince: ago(days(10)), URL: "/" + fixtureUser + "/hollow-atlas/pulls/41",
			Actor: "ada-silva",
		}},
		AttentionTotal: 3,
		Activity: []model.Event{{
			ID: 1, ActorName: "ada-silva", RepoName: "hollow-atlas", OwnerName: fixtureUser,
			EventType: "push", CreatedAt: ago(days(11)),
		}},
	}
}
