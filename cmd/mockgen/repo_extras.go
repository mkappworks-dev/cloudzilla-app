package main

import (
	"context"
	"time"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const extrasRepoName = "hollow-atlas"

// extrasRepo is the repository the settings, wiki, project, release and gist-adjacent pages are drawn for.
func extrasRepo() model.Repository { return fixtureRepos()[0] }

// extrasRepoBase adds the repository subnav that withRepoSubnav builds in the handler.
func extrasRepoBase(b view.BasePage, repo model.Repository, active string, canManage bool) view.BasePage {
	b.OwnerContext = repo.OwnerName
	b.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName: repo.OwnerName, RepoName: repo.Name, Active: active, CanManage: canManage, Private: repo.Private,
		AllowIssues: repo.AllowIssues, AllowDiscussions: repo.AllowDiscussions,
		AllowProjects: repo.AllowProjects, AllowWiki: repo.AllowWiki,
		Counts: map[string]int{"pull_requests": 2, "issues": 7, "discussions": 3, "releases": 3},
	}
	for _, r := range fixtureRepos() {
		b.RepoSwitcher = append(b.RepoSwitcher, view.RepoRef{Name: r.Name, Path: "/" + r.OwnerName + "/" + r.Name})
	}
	return b
}

func extrasWikiPages() []service.WikiPageMeta {
	return []service.WikiPageMeta{
		{Slug: "Home", Title: "Home", UpdatedAt: ago(days(9))},
		{Slug: "Setup-on-Mac", Title: "Setup on Mac", UpdatedAt: ago(days(14))},
		{Slug: "Architecture", Title: "Architecture", UpdatedAt: ago(days(20))},
		{Slug: "Contributing", Title: "Contributing", UpdatedAt: ago(days(30))},
		{Slug: "Code-of-Conduct", Title: "Code of Conduct", UpdatedAt: ago(days(60))},
		{Slug: "Release-process", Title: "Release process", UpdatedAt: ago(days(45))},
	}
}

const extrasWikiHome = `# Hollow Atlas

Hollow Atlas serves **offline-first map tiles** to fleets that cannot count on a network.

- Setup on Mac gets a tile server running locally.
- Architecture explains how tiles are cached and synced.
`

func extrasWiki(b view.BasePage, canWrite, canManage, exists bool) templ.Component {
	repo := extrasRepo()
	d := view.WikiPageData{
		BasePage: extrasRepoBase(b, repo, "wiki", canManage), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
		Slug: "Home", PageList: extrasWikiPages(), CanWrite: canWrite, CanManage: canManage, Exists: exists,
	}
	if exists {
		d.ContentHTML = markdown.RenderCtx(context.Background(), extrasWikiHome)
	} else {
		d.Slug = "Deployment"
	}
	return pages.WikiPage(d)
}

func extrasProject() model.Project {
	return model.Project{
		ID: 1, RepoID: 1, Name: "Tile cache v2", Description: "Rework the on-disk tile index and its sync path.",
		CreatedAt: ago(days(40)), UpdatedAt: ago(days(9)),
	}
}

func extrasLabels() []model.Label {
	return []model.Label{
		{ID: 1, RepoID: 1, Name: "bug", Color: "#d73a4a", Description: "Something is not working"},
		{ID: 2, RepoID: 1, Name: "enhancement", Color: "#a2eeef", Description: "New feature or request"},
		{ID: 3, RepoID: 1, Name: "docs", Color: "#0075ca", Description: "Documentation"},
	}
}

func extrasPeople() []model.CardUser {
	return []model.CardUser{{ID: 1, Username: fixtureUser}, {ID: 2, Username: "ada-silva"}, {ID: 3, Username: "tomas-ruiz"}, {ID: 4, Username: "wei-chen"}, {ID: 5, Username: "ines-novak"}, {ID: 6, Username: "felix-eriksen"}, {ID: 7, Username: "alan-kowalski"}}
}

func extrasColumns() []service.KanbanColumnView {
	labels := extrasLabels()
	ada := []model.CardUser{{ID: 2, Username: "ada-silva"}}
	return []service.KanbanColumnView{
		{ID: 1, Name: "To do", Cards: []service.KanbanCardView{
			{ID: 1, Title: "Benchmark the tile index on a 64 GB region", Kind: "note", ColumnID: 1,
				Description: "Compare cold and warm reads before choosing the page size.", DescriptionHTML: "<p>Compare cold and warm reads before choosing the page size.</p>",
				DueDate: "2025-06-20", Labels: labels[1:2], Assignees: ada},
			{ID: 2, Title: "Tile sync stalls after a dropped connection", Kind: "issue", State: "open", ColumnID: 1,
				LinkKind: "issue", LinkID: 12, LinkNumber: 12, LinkState: "open", LinkTitle: "Tile sync stalls after a dropped connection",
				RepoFullName: fixtureUser + "/" + extrasRepoName, Labels: labels[0:1]},
		}},
		{ID: 2, Name: "In progress", Cards: []service.KanbanCardView{
			{ID: 3, Title: "Cache tile index on disk", Kind: "pull", State: "open", ColumnID: 2,
				LinkKind: "pull", LinkID: 41, LinkNumber: 41, LinkState: "open", LinkTitle: "Cache tile index on disk",
				RepoFullName: fixtureUser + "/" + extrasRepoName, Assignees: ada, DueDate: "2025-06-01", Overdue: true},
			{ID: 4, Title: "Draft the migration note", Kind: "note", ColumnID: 2, Labels: labels[2:3]},
		}},
		{ID: 3, Name: "Done", Cards: []service.KanbanCardView{
			{ID: 5, Title: "Prune stale tiles on startup", Kind: "issue", State: "closed", ColumnID: 3,
				LinkKind: "issue", LinkID: 9, LinkNumber: 9, LinkState: "closed", LinkTitle: "Prune stale tiles on startup",
				RepoFullName: fixtureUser + "/" + extrasRepoName},
			{ID: 6, Title: "Linked item in a private repository", Kind: "hidden", ColumnID: 3},
		}},
	}
}

func extrasProjectBoard(b view.BasePage, canWrite bool) templ.Component {
	repo := extrasRepo()
	d := view.ProjectDetailData{
		BasePage: extrasRepoBase(b, repo, "projects", canWrite), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
		Project: extrasProject(), Columns: extrasColumns(), CanWrite: canWrite, CanManage: canWrite,
	}
	if canWrite {
		d.Labels, d.People = extrasLabels(), extrasPeople()
	}
	return pages.ProjectDetail(d)
}

func extrasProjects(b view.BasePage, canWrite bool, items []service.ProjectListView, open, closed int) templ.Component {
	repo := extrasRepo()
	return pages.Projects(view.ProjectsData{
		BasePage: extrasRepoBase(b, repo, "projects", canWrite), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
		Items: items, StateFilter: "open", OpenCount: open, ClosedCount: closed, CanWrite: canWrite, CanManage: canWrite,
	})
}

func extrasProjectItems() []service.ProjectListView {
	closedAt := ago(days(20))
	roadmap := extrasProject()
	roadmap.ID, roadmap.Name, roadmap.Description = 2, "Q3 roadmap", ""
	return []service.ProjectListView{
		{Project: extrasProject(), CardCount: 6, LinkedCount: 4, DoneCount: 1},
		{Project: roadmap, CardCount: 3},
		{Project: model.Project{ID: 3, RepoID: 1, Name: "Offline sync beta", Description: "Shipped in v0.4.", CreatedAt: ago(days(90)), UpdatedAt: ago(days(20)), ClosedAt: &closedAt}, CardCount: 8, LinkedCount: 8, DoneCount: 8},
	}
}

func extrasReleases() []view.ReleaseView {
	pub := func(d time.Duration) *time.Time { t := ago(d); return &t }
	return []view.ReleaseView{
		{Release: model.Release{ID: 4, RepoID: 1, TagName: "v0.5.0-rc1", Name: "v0.5.0 release candidate", IsDraft: true, AuthorID: 1, CreatedAt: ago(days(2)), UpdatedAt: ago(days(2))}, Excerpt: "Tile index v2 behind a flag. Not for production."},
		{Release: model.Release{ID: 3, RepoID: 1, TagName: "v0.4.1", Name: "v0.4.1", AuthorID: 1, CreatedAt: ago(days(14)), PublishedAt: pub(days(14))}, IsLatest: true, Excerpt: "Fixes a sync stall after a dropped connection and prunes stale tiles on startup."},
		{Release: model.Release{ID: 2, RepoID: 1, TagName: "v0.5.0-beta", Name: "", IsPrerelease: true, AuthorID: 1, CreatedAt: ago(days(30)), PublishedAt: pub(days(30))}, Excerpt: "First build with the offline sync path."},
		{Release: model.Release{ID: 1, RepoID: 1, TagName: "v0.4.0", Name: "v0.4.0", AuthorID: 1, CreatedAt: ago(days(60)), PublishedAt: pub(days(60))}, Excerpt: "Elevation layer and weekly road refresh."},
	}
}

func extrasReleasesPage(b view.BasePage, canWrite bool, rel []view.ReleaseView) templ.Component {
	repo := extrasRepo()
	return pages.Releases(view.ReleasesData{
		BasePage: extrasRepoBase(b, repo, "releases", canWrite), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
		Releases: rel, CanWrite: canWrite,
	})
}

const extrasReleaseBody = `## What's new

- Sync resumes where it stopped after a dropped connection (#12).
- Stale tiles are pruned on startup (#9).

## Upgrading

Run ` + "`atlas migrate`" + ` once before starting the new binary.
`

func extrasReleaseDetail(b view.BasePage, canWrite bool, mutate func(*view.ReleaseDetailData)) templ.Component {
	repo := extrasRepo()
	pubAt := ago(days(14))
	d := view.ReleaseDetailData{
		BasePage: extrasRepoBase(b, repo, "releases", canWrite), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
		Release:  model.Release{ID: 3, RepoID: 1, TagName: "v0.4.1", Name: "v0.4.1", Body: extrasReleaseBody, AuthorID: 1, CreatedAt: ago(days(14)), PublishedAt: &pubAt},
		BodyHTML: markdown.RenderCtx(context.Background(), extrasReleaseBody), CanWrite: canWrite,
		AuthorName: fixtureUser, IsLatest: true, CommitSHA: "3f9c2a7d41be8a60c5d19e7f2b84a3c06d5e1f78",
	}
	if mutate != nil {
		mutate(&d)
	}
	return pages.ReleaseDetail(d)
}

func extrasGistRows() []model.GistListRow {
	return []model.GistListRow{
		{Gist: model.Gist{ID: "a1b2c3d4e5f6", OwnerID: 1, OwnerName: fixtureUser, Description: "Retry with jitter in Go", Public: true, CreatedAt: ago(days(30)), UpdatedAt: ago(days(9))}, FileCount: 1, StarCount: 4, ForkCount: 1},
		{Gist: model.Gist{ID: "b2c3d4e5f6a1", OwnerID: 2, OwnerName: "ada-silva", Description: "Tile-seed script", Public: true, CreatedAt: ago(days(40)), UpdatedAt: ago(days(15))}, FileCount: 2, StarCount: 0},
		{Gist: model.Gist{ID: "c3d4e5f6a1b2", OwnerID: 1, OwnerName: fixtureUser, Public: false, CreatedAt: ago(days(50)), UpdatedAt: ago(days(40))}, FileCount: 1, StarCount: 1},
	}
}

const (
	chipPrimary = "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-primary/15 text-primary border-primary/30 whitespace-nowrap"
	chipSuccess = "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-success/15 text-success border-success/30 whitespace-nowrap"
	chipMuted   = "inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border border-border bg-accent text-muted-foreground whitespace-nowrap"
)

// gistsAccount adds the account subnav the signed-in gist pages carry, with the Gists tab counted.
func gistsAccount(b view.BasePage) view.BasePage {
	if b.CurrentUser == nil {
		return b
	}
	b = onAccountPage(b, "gists")
	b.AccountSubnav.Counts["gists"] = 3
	return b
}

func extrasGists(b view.BasePage, tab string, rows []model.GistListRow, signedIn bool, public int) templ.Component {
	labels := []struct{ label, class string }{{"Go", chipPrimary}, {"Bash", chipSuccess}, {"Markdown", chipMuted}}
	items := make([]view.GistListItem, 0, len(rows))
	for i, r := range rows {
		l := labels[i%len(labels)]
		items = append(items, view.GistListItem{GistListRow: r, LanguageLabel: l.label, LanguageClass: l.class})
	}
	return pages.Gists(view.GistsData{
		BasePage: gistsAccount(b), Gists: items, Page: 1, Tab: tab, Sort: "updated", PrivateTabAvailable: signedIn,
		PublicCount: public, PrivateCount: 1,
	})
}

func extrasGist() (model.Gist, []model.GistFile) {
	g := extrasGistRows()[0].Gist
	return g, []model.GistFile{
		{ID: 1, GistID: g.ID, Filename: "retry.go", Content: "package retry\n\nimport (\n\t\"math/rand\"\n\t\"time\"\n)\n\n// Backoff returns the wait before attempt n, with up to 50% jitter.\nfunc Backoff(n int) time.Duration {\n\tbase := time.Duration(1<<n) * 100 * time.Millisecond\n\treturn base/2 + time.Duration(rand.Int63n(int64(base/2)))\n}\n"},
		{ID: 2, GistID: g.ID, Filename: "NOTES.md", Content: "Use this for **idempotent** calls only.\n"},
	}
}

func repoExtrasPages() []page {
	repo := extrasRepo()
	settings := func(mutate func(*view.RepoSettingsData)) func(view.BasePage) templ.Component {
		return func(b view.BasePage) templ.Component {
			ts := ago(days(20))
			d := view.RepoSettingsData{
				BasePage: extrasRepoBase(b, repo, "settings", true), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
				Webhooks: []model.Webhook{{ID: 1, RepoID: 1, URL: "https://ci.example.test/hooks/atlas", Events: "push,pull_request", Active: true, CreatedAt: ts}},
				Collabs: []model.Permission{
					{ID: 1, UserID: 2, RepoID: 1, Role: model.RoleWriter, Username: "ada-silva", CreatedAt: ts},
					{ID: 2, UserID: 3, RepoID: 1, Role: model.RoleReader, Username: "tomas-ruiz", CreatedAt: ts},
				},
				Labels:     extrasLabels(),
				DeployKeys: []model.DeployKey{{ID: 1, RepoID: 1, Title: "ci-runner", Fingerprint: "SHA256:Zk3p0w5mQ1v8hG7aYd2uXb4sL9eN6cTfRj1oIiWqA0E", ReadOnly: true, CreatedAt: ts}},
				BranchProtections: []*model.BranchProtection{{
					ID: 1, RepoID: 1, Pattern: "main", RequirePullRequest: true, RequireReviewCount: 1,
					RequireStatusChecks: model.StringSlice{"ci/test"}, BlockForcePush: true, CreatedAt: ts, UpdatedAt: ts,
				}},
				CanManage: true, IsOwner: true, Confirm: components.ConfirmFactors{Password: true},
			}
			mutate(&d)
			return pages.RepoSettings(d)
		}
	}
	return []page{
		{
			Name: "settings", Route: "/{owner}/{repo}/settings", Templ: "repo_settings.templ", Title: "Repository settings",
			Variants: []variant{
				{Note: "repository owner", Viewer: Writer, Render: settings(func(*view.RepoSettingsData) {})},
				{Name: "manager", Note: "can manage but does not own: no transfer, delete or visibility controls", Viewer: Writer,
					Render: settings(func(d *view.RepoSettingsData) { d.IsOwner = false })},
				{Name: "archived", Note: "archived repository banner and Unarchive", Viewer: Writer, Render: settings(func(d *view.RepoSettingsData) {
					at := ago(days(5))
					d.Repo.IsArchived, d.Repo.ArchivedAt = true, &at
				})},
				{Name: "mirror", Note: "pull mirror: remote, interval, token, last sync", Viewer: Writer, Render: settings(func(d *view.RepoSettingsData) {
					d.Repo.IsMirror = true
					d.Mirror = &view.MirrorSettings{
						RemoteURL: "https://git.example.test/atlas/hollow-atlas.git", AuthUsername: "mirror-bot", HasToken: true,
						Intervals:  []view.IntervalOption{{Value: "1h", Label: "Every hour"}, {Value: "6h", Label: "Every 6 hours", Selected: true}, {Value: "24h", Label: "Every day"}},
						LastSynced: "2 hours ago", NextSync: "in 4 hours",
						APIURL: "/api/repos/" + fixtureUser + "/" + extrasRepoName + "/mirror", SyncURL: "/api/repos/" + fixtureUser + "/" + extrasRepoName + "/mirror/sync",
					}
				})},
				{Name: "mirror-failing", Note: "pull mirror whose last sync failed", Viewer: Writer, Render: settings(func(d *view.RepoSettingsData) {
					d.Repo.IsMirror = true
					d.Mirror = &view.MirrorSettings{
						RemoteURL:  "https://git.example.test/atlas/hollow-atlas.git",
						Intervals:  []view.IntervalOption{{Value: "6h", Label: "Every 6 hours", Selected: true}},
						LastSynced: "2 days ago", NextSync: "in 10 minutes", Failing: true, LastError: "authentication failed: remote rejected the token",
						APIURL: "/api/repos/" + fixtureUser + "/" + extrasRepoName + "/mirror", SyncURL: "/api/repos/" + fixtureUser + "/" + extrasRepoName + "/mirror/sync",
					}
				})},
				{Name: "transfer", Note: "ownership transfer awaiting its recipient", Viewer: Writer, Render: settings(func(d *view.RepoSettingsData) {
					d.PendingTransfer = &model.RepoTransfer{
						ID: 1, RepoID: 1, OwnerName: fixtureUser, RepoName: extrasRepoName, RequesterID: 1, RequesterName: fixtureUser,
						RecipientID: 2, RecipientName: "ada-silva", ExpiresAt: fixedNow.Add(days(6)), CreatedAt: ago(days(1)),
					}
				})},
			},
		},
		{
			Name: "wiki", Route: "/{owner}/{repo}/wiki/{slug}", Templ: "wiki_page.templ", Title: "Wiki",
			Variants: []variant{
				{Note: "repository owner: Edit, Delete, New page and page reordering", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return extrasWiki(b, true, true, true) }},
				{Name: "writer", Note: "write access without manage: Edit and New page, no Delete or reordering", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return extrasWiki(b, true, false, true) }},
				{Name: "viewer", Note: "read-only", Viewer: Reader, Render: func(b view.BasePage) templ.Component { return extrasWiki(b, false, false, true) }},
				{Name: "empty", Note: "page does not exist yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return extrasWiki(b, true, true, false) }},
			},
		},
		{
			Name: "wiki_edit", Route: "/{owner}/{repo}/wiki/{slug}/edit", Templ: "wiki_edit.templ", Title: "Edit wiki page",
			Variants: []variant{{Note: "editing Home", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
				return pages.WikiEdit(view.WikiEditData{
					BasePage: extrasRepoBase(b, repo, "wiki", true), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
					Slug: "Home", Content: extrasWikiHome, PageList: extrasWikiPages(), CanWrite: true,
				})
			}}},
		},
		{
			Name: "wiki_new", Route: "/{owner}/{repo}/wiki/new", Templ: "wiki_new.templ", Title: "New wiki page",
			Variants: []variant{{Note: "blank form", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
				return pages.WikiNew(view.WikiNewData{
					BasePage: extrasRepoBase(b, repo, "wiki", true), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
					PageList: extrasWikiPages(),
				})
			}}},
		},
		{
			Name: "project", Route: "/{owner}/{repo}/projects", Templ: "projects.templ", Title: "Projects",
			Variants: []variant{
				{Note: "open boards, writer", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasProjects(b, true, extrasProjectItems()[:2], 2, 1)
				}},
				{Name: "viewer", Note: "signed in, read-only", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return extrasProjects(b, false, extrasProjectItems()[:2], 2, 1)
				}},
				{Name: "anon", Note: "signed out", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return extrasProjects(b, false, extrasProjectItems()[:2], 2, 1)
				}},
				{Name: "empty", Note: "no boards yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasProjects(b, true, nil, 0, 0)
				}},
			},
		},
		{
			Name: "project_board", Route: "/{owner}/{repo}/projects/{id}", Templ: "project_detail.templ", Title: "Project board",
			Variants: []variant{
				{Note: "writer: drag, edit and add", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return extrasProjectBoard(b, true) }},
				{Name: "viewer", Note: "read-only", Viewer: Reader, Render: func(b view.BasePage) templ.Component { return extrasProjectBoard(b, false) }},
			},
		},
		{
			Name: "releases", Route: "/{owner}/{repo}/releases", Templ: "releases.templ", Title: "Releases",
			Variants: []variant{
				{Note: "writer: draft, latest, prerelease and plain releases", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasReleasesPage(b, true, extrasReleases())
				}},
				{Name: "viewer", Note: "read-only: drafts are hidden by the server, so none here", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return extrasReleasesPage(b, false, extrasReleases()[1:])
				}},
				{Name: "empty", Note: "no releases yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasReleasesPage(b, true, nil)
				}},
			},
		},
		{
			Name: "release_detail", Route: "/{owner}/{repo}/releases/tag/{tag}", Templ: "release_detail.templ", Title: "Release",
			Variants: []variant{
				{Note: "latest release, writer", Viewer: Writer, Render: func(b view.BasePage) templ.Component { return extrasReleaseDetail(b, true, nil) }},
				{Name: "viewer", Note: "read-only", Viewer: Reader, Render: func(b view.BasePage) templ.Component { return extrasReleaseDetail(b, false, nil) }},
				{Name: "prerelease", Note: "marked as a prerelease", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasReleaseDetail(b, true, func(d *view.ReleaseDetailData) { d.IsLatest, d.Release.IsPrerelease = false, true })
				}},
				{Name: "draft", Note: "unpublished draft with Publish", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasReleaseDetail(b, true, func(d *view.ReleaseDetailData) {
						d.IsLatest, d.Release.IsDraft, d.Release.PublishedAt, d.CommitSHA = false, true, nil, ""
					})
				}},
			},
		},
		{
			Name: "release_new", Route: "/{owner}/{repo}/releases/new", Templ: "release_new.templ", Title: "New release",
			Variants: []variant{{Note: "blank form", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
				return pages.ReleaseNew(view.ReleaseNewData{
					BasePage: extrasRepoBase(b, repo, "releases", true), Repo: repo, Owner: repo.OwnerName, RepoName: repo.Name,
					Branches: []service.BranchInfo{{Name: "main", IsDefault: true}, {Name: "release/0.5"}},
				})
			}}},
		},
		{
			Name: "gists", Route: "/gists", Templ: "gists.templ", Title: "Gists",
			Variants: []variant{
				{Note: "public gists, signed in", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasGists(b, "public", extrasGistRows()[:2], true, 2)
				}},
				{Name: "private", Note: "your private gists", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasGists(b, "private", extrasGistRows()[2:], true, 2)
				}},
				{Name: "anon", Note: "signed out: no private tab", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return extrasGists(b, "public", extrasGistRows()[:2], false, 2)
				}},
				{Name: "empty", Note: "no public gists", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return extrasGists(b, "public", nil, true, 0)
				}},
			},
		},
		{
			Name: "gist_detail", Route: "/gists/{id}", Templ: "gist_detail.templ", Title: "Gist",
			Variants: []variant{
				{Note: "owner: Edit and Delete", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					g, f := extrasGist()
					return pages.GistDetail(view.GistDetailData{BasePage: gistsAccount(b), Gist: g, Files: f, IsOwner: true})
				}},
				{Name: "viewer", Note: "someone else's gist", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					g, f := extrasGist()
					g.OwnerID, g.OwnerName = 2, "ada-silva"
					return pages.GistDetail(view.GistDetailData{BasePage: gistsAccount(b), Gist: g, Files: f})
				}},
			},
		},
		{
			Name: "gist_edit", Route: "/gists/{id}/edit", Templ: "gist_edit.templ", Title: "Edit gist",
			Variants: []variant{{Note: "two files", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
				g, f := extrasGist()
				return pages.GistEdit(view.GistEditData{BasePage: gistsAccount(b), Gist: g, Files: f})
			}}},
		},
		{
			Name: "gist_new", Route: "/gists/new", Templ: "gist_new.templ", Title: "New gist",
			Variants: []variant{{Note: "blank form", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
				return pages.GistNew(view.GistNewData{BasePage: gistsAccount(b)})
			}}},
		},
	}
}
