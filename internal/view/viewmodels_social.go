package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

type RenderedDiscussionReply struct {
	model.DiscussionReply
	BodyHTML string
}

type DiscussionsData struct {
	BasePage
	Repo              model.Repository
	Owner             string
	RepoName          string
	Categories        []model.DiscussionCategory
	Discussions       []model.Discussion
	ActiveCategoryID  int64
	CategoryCounts    map[int64]int
	TotalCount        int
	StateFilter       string
	OpenCount         int
	AnsweredCount     int
	ClosedCount       int
	CanWrite          bool
}

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

type GistsData struct {
	BasePage
	Gists []model.Gist
	Page  int
}

type GistDetailData struct {
	BasePage
	Gist    model.Gist
	Files   []model.GistFile
	IsOwner bool
}

type GistNewData struct {
	BasePage
}

type GistEditData struct {
	BasePage
	Gist  model.Gist
	Files []model.GistFile
}

type StargazersData struct {
	BasePage
	Repo       model.Repository
	Owner      string
	RepoName   string
	Stargazers []model.User
	StarCount  int
}

type SearchData struct {
	BasePage
	Query   string
	Type    string
	Results *service.SearchResults
}

type TopicData struct {
	BasePage
	TopicName string
	Repos     []model.Repository
	Page      int
}

type CodeSearchData struct {
	BasePage
	Query      string
	RepoFilter string
	Lang       string
	Results    []model.CodeSearchResult
	Total      int
	Page       int
}

type ExploreData struct {
	BasePage
	Tab    string
	Period string
	Repos  []model.RepositoryWithStats
}

type DependenciesData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Groups   []components.DependencyGroupData
}

type ProjectsData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
	Projects  []model.Project
	CanWrite  bool
	CanManage bool
}

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

type WikiPageData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Slug        string
	ContentHTML string
	PageList    []string
	CanWrite    bool
	CanManage   bool
	Exists      bool
}

type WikiEditData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Slug     string
	Content  string
	CanWrite bool
}

type PulseData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	IssuesOpened  int
	IssuesClosed  int
	PullsOpened   int
	PullsMerged   int
	RecentCommits int
	CommitsLast30 []int
	IssuesLast30  []int
	PullsLast30   []int
	Contributors  []service.ContributorWithTimeline
}

type ContributorsData struct {
	BasePage
	Repo *model.Repository
	Rows []service.ContributorWithTimeline
}

type ActionsData struct {
	BasePage
	Repo      model.Repository
	Owner     string
	RepoName  string
}
