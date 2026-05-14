package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// RenderedDiscussionReply wraps a DiscussionReply with its body pre-rendered as HTML.
// RenderedDiscussionReply holds a discussion reply pre-rendered to safe HTML.
type RenderedDiscussionReply struct {
	model.DiscussionReply
	BodyHTML string
}

// DiscussionsData is the view model for /{owner}/{repo}/discussions
// DiscussionsData holds template data for the discussions list page.
type DiscussionsData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	Categories       []model.DiscussionCategory
	Discussions      []model.Discussion
	ActiveCategoryID int64
	CanWrite         bool
}

// DiscussionDetailData is the view model for /{owner}/{repo}/discussions/{number}
// DiscussionDetailData holds template data for the discussion detail page.
type DiscussionDetailData struct {
	BasePage
	Repo       model.Repository
	Owner      string
	RepoName   string
	Discussion model.Discussion
	Category   model.DiscussionCategory
	Replies    []RenderedDiscussionReply
	BodyHTML   string
	CanWrite   bool
}

// GistsData is the view model for /gists (explore page)
// GistsData holds template data for the gist list page.
type GistsData struct {
	BasePage
	Gists []model.Gist
	Page  int
}

// GistDetailData is the view model for /gists/{id}
// GistDetailData holds template data for the gist detail page.
type GistDetailData struct {
	BasePage
	Gist    model.Gist
	Files   []model.GistFile
	IsOwner bool
}

// GistNewData is the view model for /gists/new
// GistNewData holds template data for the new gist form page.
type GistNewData struct {
	BasePage
}

// GistEditData is the view model for /gists/{id}/edit
// GistEditData holds template data for the gist edit form page.
type GistEditData struct {
	BasePage
	Gist  model.Gist
	Files []model.GistFile
}

// StargazersData page
// StargazersData holds template data for the repository stargazers page.
type StargazersData struct {
	BasePage
	Repo       model.Repository
	Owner      string
	RepoName   string
	Stargazers []model.User
	StarCount  int
}

// SearchData page
// SearchData holds template data for the search results page.
type SearchData struct {
	BasePage
	Query   string
	Type    string
	Results *service.SearchResults
}

// TopicData is the view model for the /topic/{name} explore page.
// TopicData holds template data for the topic explore page.
type TopicData struct {
	BasePage
	TopicName string
	Repos     []model.Repository
	Page      int
}

// CodeSearchData is the view model for the /search/code page.
// CodeSearchData holds template data for the code search results page.
type CodeSearchData struct {
	BasePage
	Query      string
	RepoFilter string
	Lang       string
	Results    []model.CodeSearchResult
	Total      int
	Page       int
}

// ExploreData is the view model for the /explore page.
// ExploreData holds template data for the explore/trending page.
type ExploreData struct {
	BasePage
	Tab    string
	Period string
	Repos  []model.RepositoryWithStats
}

// DependenciesData is the view model for the /{owner}/{repo}/network/dependencies page.
// DependenciesData holds template data for the dependency graph page.
type DependenciesData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	Dependencies []model.RepoDependency
	ByManager    map[string][]model.RepoDependency
}

// ProjectsData is the view model for the projects list page.
// ProjectsData holds template data for the project boards list page.
type ProjectsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Projects []model.Project
	CanWrite bool
}

// ProjectDetailData is the view model for the Kanban board page.
// ProjectDetailData holds template data for the Kanban project board detail page.
type ProjectDetailData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	Project   model.Project
	Columns   []service.KanbanColumnView
	CanWrite  bool
	CanManage bool
}

// Wiki page view (read mode)
// WikiPageData holds template data for a wiki page view.
type WikiPageData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Slug        string
	ContentHTML string // rendered HTML from markdown, use @templ.Raw(data.ContentHTML) in template
	PageList    []string
	CanWrite    bool
	CanManage   bool // true for owners and admin collaborators; gates the Delete button
	Exists      bool // false when the page has never been created
}

// Wiki editor (create / edit mode)
// WikiEditData holds template data for the wiki page editor.
type WikiEditData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Slug     string
	Content  string
	CanWrite bool
}

// PulseData is used by the /{owner}/{repo}/pulse page.
// PulseData holds template data for the repository pulse/insights page.
type PulseData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	NewIssues     int
	ClosedIssues  int
	NewPRs        int
	MergedPRs     int
	OpenPRs       int
	RecentCommits int
	Contributors  []service.ContributorStat
}

// ContributorsData is used by the /{owner}/{repo}/graphs/contributors page.
// ContributorsData holds template data for the contributors graph page.
type ContributorsData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	Contributors []service.ContributorStat
	MaxCommits   int
}
