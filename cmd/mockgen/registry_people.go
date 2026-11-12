package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func peoplePages() []page {
	return []page{
		{
			Name: "profile", Route: "/{user}", Templ: "user.templ", Title: "Profile",
			Variants: []variant{
				vr("", "your own profile", Writer, func(b view.BasePage) templ.Component {
					return pages.User(userData(b))
				}),
				vr("other", "someone else's profile", Reader, func(b view.BasePage) templ.Component {
					d := userData(b)
					d.IsOwnProfile = false
					d.User.ID = 2
					d.User.Username = "ada-silva"
					d.User.Name = "Ada Silva"
					d.User.Email = ""
					return pages.User(d)
				}),
				vr("empty", "new account with nothing yet", Writer, func(b view.BasePage) templ.Component {
					d := view.UserData{BasePage: b, User: fixtureUserRow(), IsOwnProfile: true, Tab: "overview"}
					return pages.User(d)
				}),
			},
		},
		{
			Name: "org_profile", Route: "/{org}", Templ: "org.templ", Title: "Organization",
			Variants: []variant{
				vr("", "owner viewing their organization", Writer, func(b view.BasePage) templ.Component {
					return pages.Org(orgData(b))
				}),
				vr("visitor", "signed-out visitor", Anon, func(b view.BasePage) templ.Component {
					d := orgData(b)
					d.CanManage = false
					d.ViewerRole = nil
					d.ViewerJoined = nil
					return pages.Org(d)
				}),
				vr("empty", "nothing pinned, no repositories", Writer, func(b view.BasePage) templ.Component {
					d := orgData(b)
					d.PinnedRepos, d.RecentRepos, d.TopLangs = nil, nil, nil
					return pages.Org(d)
				}),
			},
		},
		{
			Name: "organizations", Route: "/organizations", Templ: "organizations.templ", Title: "Organizations",
			Variants: []variant{
				vr("", "organizations you belong to", Writer, func(b view.BasePage) templ.Component {
					return pages.Organizations(view.OrgListData{BasePage: b, Entries: []view.OrgListEntry{
						{Org: fixtureOrg(), Role: model.OrgRoleOwner, MemberCount: 4},
						{Org: model.Organization{ID: 2, Name: "tidepool", DisplayName: "Tidepool", Description: "Observability for small teams."}, Role: model.OrgRoleMember, MemberCount: 9},
					}})
				}),
				vr("empty", "no organizations yet", Writer, func(b view.BasePage) templ.Component {
					return pages.Organizations(view.OrgListData{BasePage: b})
				}),
			},
		},
		{
			Name: "new_organization", Route: "/organizations/new", Templ: "new_organization.templ", Title: "New organization",
			Variants: []variant{
				vr("", "empty form", Writer, func(b view.BasePage) templ.Component {
					return pages.NewOrganization(view.NewOrganizationData{BasePage: b})
				}),
				vr("error", "name already taken", Writer, func(b view.BasePage) templ.Component {
					return pages.NewOrganization(view.NewOrganizationData{BasePage: b, Name: "paper-planes", Description: "Small tools.", Error: "That name is already taken."})
				}),
			},
		},
		{
			Name: "my_repositories", Route: "/repos", Templ: "account_repos.templ", Title: "Your repositories",
			Variants: []variant{
				vr("", "all repositories", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountRepos(reposData(b))
				}),
				vr("transfers", "a repository transfer is waiting", Writer, func(b view.BasePage) templ.Component {
					d := reposData(b)
					d.IncomingTransfers = 1
					return pages.AccountRepos(d)
				}),
				vr("empty", "no repositories yet", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountRepos(view.AccountReposData{BasePage: onAccountPage(b, "repositories"), Filter: "all", Sort: "updated", UserID: 1, TypeCounts: map[string]int{}})
				}),
			},
		},
		{
			Name: "my_pulls", Route: "/pulls", Templ: "account_pulls.templ", Title: "Your pull requests",
			Variants: []variant{
				vr("", "created by you, open", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountPulls(pullsData(b))
				}),
				vr("empty", "nothing open", Writer, func(b view.BasePage) templ.Component {
					d := pullsData(b)
					d.Pulls = nil
					return pages.AccountPulls(d)
				}),
			},
		},
		{
			Name: "my_issues", Route: "/issues", Templ: "account_issues.templ", Title: "Your issues",
			Variants: []variant{
				vr("", "assigned to you, open", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountIssues(issuesData(b))
				}),
				vr("empty", "nothing open", Writer, func(b view.BasePage) templ.Component {
					d := issuesData(b)
					d.Issues = nil
					return pages.AccountIssues(d)
				}),
			},
		},
		{
			Name: "attention", Route: "/attention", Templ: "account_attention.templ", Title: "Needs your attention",
			Variants: []variant{
				vr("", "items waiting on you", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountAttention(attentionData(b, attentionItems()))
				}),
				vr("empty", "nothing waiting", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountAttention(attentionData(b, nil))
				}),
			},
		},
		{
			Name: "notifications", Route: "/notifications", Templ: "notifications.templ", Title: "Notifications",
			Variants: []variant{
				vr("", "inbox with unread items", Writer, func(b view.BasePage) templ.Component {
					return pages.Notifications(notificationsData(b, fixtureNotifications()))
				}),
				vr("empty", "inbox zero", Writer, func(b view.BasePage) templ.Component {
					return pages.Notifications(notificationsData(b, nil))
				}),
			},
		},
		{
			Name: "stars", Route: "/stars", Templ: "account_stars.templ", Title: "Your stars",
			Variants: []variant{
				vr("", "starred repositories", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountStars(view.AccountStarsData{BasePage: b, Username: fixtureUser, Stars: fixtureRepos()[:3], Languages: []string{"Go", "Markdown", "Rust"}})
				}),
				vr("empty", "nothing starred", Writer, func(b view.BasePage) templ.Component {
					return pages.AccountStars(view.AccountStarsData{BasePage: b, Username: fixtureUser})
				}),
			},
		},
		{
			Name: "new_repo", Route: "/repos/new", Templ: "repo_new.templ", Title: "New repository",
			Variants: []variant{
				vr("", "create form", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoNew(view.RepoNewData{
						BasePage: onAccountPage(b, "repositories"), OwnedOrgs: []model.Organization{fixtureOrg()},
						GitignoreTemplates: []string{"Go", "Node", "Python", "Rust"},
						LicenseTemplates:   []service.License{{Key: "mit", Name: "MIT License"}, {Key: "apache-2.0", Name: "Apache License 2.0"}},
					})
				}),
			},
		},
		{
			Name: "repo_import", Route: "/repos/import", Templ: "repo_import.templ", Title: "Import a repository",
			Variants: []variant{
				vr("", "import form", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoImport(view.RepoImportData{BasePage: onAccountPage(b, "repositories"), OwnedOrgs: []model.Organization{fixtureOrg()}})
				}),
				vr("mirror", "the instance allows pull mirrors", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoImport(view.RepoImportData{
						BasePage: onAccountPage(b, "repositories"), OwnedOrgs: []model.Organization{fixtureOrg()},
						MirrorIntervals: []view.IntervalOption{{Value: "1h", Label: "Every hour", Selected: true}, {Value: "24h", Label: "Every day"}},
					})
				}),
				vr("running", "clone in progress", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoImportStatus(view.RepoImportStatusData{BasePage: onAccountPage(b, "repositories"), Job: importJob(service.ImportRunning)})
				}),
				vr("failed", "clone failed", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoImportStatus(view.RepoImportStatusData{BasePage: onAccountPage(b, "repositories"), Job: importJob(service.ImportFailed)})
				}),
			},
		},
		{
			Name: "repo_transfers", Route: "/repos/transfers", Templ: "repo_transfers.templ", Title: "Repository transfers",
			Variants: []variant{
				vr("", "transfers offered to you", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoTransfers(view.RepoTransfersData{BasePage: b, Transfers: []view.IncomingRepoTransfer{{
						RepoTransfer: model.RepoTransfer{
							ID: 1, RepoID: 9, OwnerName: "ada-silva", RepoName: "field-notes", Description: "Notes from the road.", RequesterID: 2,
							RequesterName: "ada-silva", RecipientID: 1, RecipientName: fixtureUser, ExpiresAt: fixedNow.Add(7 * 24 * hour), CreatedAt: ago(days(1)),
						},
						Collaborators: []model.Permission{{ID: 1, UserID: 3, Username: "jonas-weber", Role: model.RoleWriter}},
					}}})
				}),
				vr("empty", "none waiting", Writer, func(b view.BasePage) templ.Component {
					return pages.RepoTransfers(view.RepoTransfersData{BasePage: b})
				}),
			},
		},
	}
}

func reposData(b view.BasePage) view.AccountReposData {
	return view.AccountReposData{
		BasePage: onAccountPage(b, "repositories"), Repos: fixtureRepos(), Filter: "all", Sort: "updated", UserID: 1, Total: 4, Page: 1, TotalPages: 1,
		TypeCounts: map[string]int{"all": 4, "owned": 4, "collaborator": 0, "forks": 0}, Languages: []string{"Go", "Markdown", "Rust", "TypeScript"},
		StarCounts: map[int64]int{1: 128, 2: 54}, CommitCounts: map[int64]int{1: 412, 2: 96, 3: 31, 4: 18},
	}
}

func pullsData(b view.BasePage) view.AccountPullsData {
	return view.AccountPullsData{
		BasePage: onAccountPage(b, "pulls"), Pulls: fixturePullItems(), Filter: "created", State: "open", Sort: "newest",
		Counts:       map[string]int{"created:open": 3, "created:closed": 8, "assigned:open": 1, "review_requested:open": 2, "mentioned:open": 0},
		PullComments: map[int64]int{1: 4}, PullLabels: map[int64][]model.Label{1: {{ID: 1, Name: "performance", Color: "0e8a16"}}},
		PullCI: map[int64]service.CIChecks{1: {Required: 3, Passing: 3}, 2: {Required: 3, Passing: 1}}, PullReviewers: map[int64][]model.PullReview{},
	}
}

func issuesData(b view.BasePage) view.AccountIssuesData {
	return view.AccountIssuesData{
		BasePage: onAccountPage(b, "issues"), Issues: fixtureIssueItems(), Filter: "assigned", State: "open", Sort: "newest",
		IssueComments: map[int64]int{1: 3}, IssueLabels: map[int64][]model.Label{1: {{ID: 2, Name: "bug", Color: "d73a4a"}}},
		Counts: map[string]int{"assigned:open": 2, "assigned:closed": 5, "created:open": 1, "created:closed": 2, "mentioned:open": 0},
	}
}

func attentionData(b view.BasePage, items []service.AttentionItem) view.AttentionData {
	return view.AttentionData{
		BasePage: onAccountPage(b, "attention"), Items: items, Kind: "all", Sort: "overdue", Total: len(items),
		Counts: map[string]int{"all": len(items), "mentions": 0, "reviews": len(items), "assigned": 0},
	}
}

func notificationsData(b view.BasePage, groups []model.NotificationGroup) view.NotificationsData {
	unread := 0
	for _, g := range groups {
		unread += g.Unread
	}
	pageCount, total := 1, len(groups)
	if total > 0 {
		pageCount, total = 9, 212
	}
	return view.NotificationsData{
		BasePage: b, Groups: groups, Filter: "inbox", Page: 1, TotalPages: pageCount, PerPage: 25, Total: total,
		InboxCount: len(groups), UnreadCount: unread, ReadCount: len(groups) - unread,
	}
}

func importJob(s service.ImportStatus) service.ImportJob {
	j := service.ImportJob{ID: "job-1", UserID: 1, SourceURL: "https://git.example.test/ada/field-notes.git", Owner: fixtureUser, Name: "field-notes", Status: s, Progress: "Receiving objects: 64%"}
	if s == service.ImportFailed {
		j.Error = "Could not reach the remote."
		j.FinishedAt = fixedNow
	}
	return j
}
