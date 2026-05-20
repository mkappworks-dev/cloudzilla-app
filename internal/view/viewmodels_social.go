package view

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

type RenderedDiscussionReply struct {
	model.DiscussionReply
	BodyHTML  string
	Reactions []model.ReactionSummary
}

type DiscussionsData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	Categories       []model.DiscussionCategory
	Discussions      []model.Discussion
	ActiveCategoryID int64
	CategoryCounts   map[int64]int
	TotalCount       int
	StateFilter      string
	OpenCount        int
	AnsweredCount    int
	ClosedCount      int
	CanWrite         bool
	Labels       map[int64][]model.Label
	ReplyCounts  map[int64]int
	Participants map[int64][]string
}

type DiscussionDetailData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	Discussion    model.Discussion
	Category      model.DiscussionCategory
	AllCategories []model.DiscussionCategory
	Labels        []model.Label
	AllLabels     []model.Label
	Replies       []RenderedDiscussionReply
	Participants  []string
	OPReactions   []model.ReactionSummary
	BodyHTML      string
	CanWrite      bool
}

type DiscussionNewData struct {
	BasePage
	Repo             model.Repository
	Owner            string
	RepoName         string
	Categories       []model.DiscussionCategory
	ActiveCategoryID int64
	Title            string
	Body             string
	Error            string
}

// GistListItem extends GistListRow with display fields derived in the handler.
type GistListItem struct {
	model.GistListRow
	LanguageLabel string
	LanguageClass string
}

type GistsData struct {
	BasePage
	Gists              []GistListItem
	Page               int
	Tab                 string // "public" or "private"
	PrivateTabAvailable bool
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
	Repos     []model.RepositoryWithStats
	Total     int
	Sort      string
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
	Repo        model.Repository
	Owner       string
	RepoName    string
	Items       []service.ProjectListView
	StateFilter string // "open" | "closed"
	SearchQuery string
	OpenCount   int
	ClosedCount int
	CanWrite    bool
	CanManage   bool
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
	PageList    []service.WikiPageMeta
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
	PageList []service.WikiPageMeta
	CanWrite bool
}

type WikiNewData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	PageList []service.WikiPageMeta
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
	Repo     model.Repository
	Owner    string
	RepoName string
}
