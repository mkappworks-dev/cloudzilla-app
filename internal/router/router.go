package router

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/handler"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/service"
)

func New(services *service.Services, cfg *config.Config, frontend fs.FS) http.Handler {
	r := chi.NewRouter()
	h := handler.New(services, cfg)

	// Global middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(true))
	r.Use(middleware.RequireSetup(services.SiteSetting))

	authMW := middleware.Auth(cfg.Auth.JWTSecret, cfg.Auth.CookieName, services.AccessToken)
	optAuthMW := middleware.OptionalAuth(cfg.Auth.JWTSecret, cfg.Auth.CookieName, services.AccessToken)

	superadminMW := middleware.RequireSuperadmin

	// Setup route (first-run wizard)
	r.Get("/setup", h.PageSetup)
	r.Post("/setup", h.PageSetupSubmit)

	// Invite routes
	r.Get("/invite/{token}", h.PageInvite)
	r.Post("/invite/{token}", h.PageInviteSubmit)

	// Admin routes
	r.With(authMW, superadminMW).Get("/admin/settings", h.PageAdminSettings)

	// Search
	r.With(optAuthMW).Get("/search", h.PageSearch)

	// Page routes
	r.Get("/", h.PageHome)
	r.Get("/login", h.PageLogin)
	r.Post("/login", h.PageLoginSubmit)
	r.With(authMW).Get("/settings", h.PageSettings)
	r.With(authMW).Get("/notifications", h.PageNotifications)
	r.With(optAuthMW).Get("/{owner}", h.PageUser)
	r.With(authMW).Get("/orgs/{org}/settings", h.PageOrgSettings)
	r.With(optAuthMW).Get("/{owner}/{repo}", h.PageRepo)
	r.With(authMW).Get("/{owner}/{repo}/settings", h.PageRepoSettings)
	r.With(optAuthMW).Get("/{owner}/{repo}/releases", h.PageReleases)
	r.With(optAuthMW).Get("/{owner}/{repo}/releases/tag/{tagName}", h.PageReleaseDetail)
	r.With(optAuthMW).Get("/{owner}/{repo}/stargazers", h.PageStargazers)
	r.With(optAuthMW).Get("/{owner}/stars", h.PageUserStars)
	r.With(optAuthMW).Get("/{owner}/{repo}/milestones", h.PageMilestones)
	r.With(optAuthMW).Get("/{owner}/{repo}/issues", h.PageIssues)
	// Issue and PR creation pages
	r.With(authMW).Get("/{owner}/{repo}/issues/new", h.PageNewIssue)
	r.With(authMW).Post("/{owner}/{repo}/issues/new", h.PageNewIssueSubmit)
	r.With(optAuthMW).Get("/{owner}/{repo}/issues/{number}", h.PageIssueDetail)
	r.With(optAuthMW).Get("/{owner}/{repo}/pulls", h.PagePulls)
	r.With(authMW).Get("/{owner}/{repo}/pulls/new", h.PageNewPull)
	r.With(authMW).Post("/{owner}/{repo}/pulls/new", h.PageNewPullSubmit)
	r.With(optAuthMW).Get("/{owner}/{repo}/pulls/{number}", h.PagePullDetail)
	r.With(optAuthMW).Get("/{owner}/{repo}/refs", h.PageRefs)
	r.With(optAuthMW).Get("/{owner}/{repo}/tree/{ref}", h.PageTree)
	r.With(optAuthMW).Get("/{owner}/{repo}/tree/{ref}/{path...}", h.PageTree)
	r.With(optAuthMW).Get("/{owner}/{repo}/blob/{ref}/{path...}", h.PageBlob)
	r.With(optAuthMW).Get("/{owner}/{repo}/blame/{ref}/{path...}", h.PageBlame)
	r.With(optAuthMW).Get("/{owner}/{repo}/commits/{ref}", h.PageCommits)
	r.With(optAuthMW).Get("/{owner}/{repo}/commits/{ref}/{path...}", h.PageCommits)
	r.With(optAuthMW).Get("/{owner}/{repo}/commit/{sha}", h.PageCommit)

	// OAuth routes
	r.Get("/auth/google", h.GoogleOAuthBegin)
	r.Get("/auth/google/callback", h.GoogleOAuthCallback)

	// Auth routes
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/login", h.Login)
		r.Post("/logout", h.Logout)
	})

	// User routes
	r.Route("/api/users", func(r chi.Router) {
		r.Use(optAuthMW)
		r.Get("/{username}", h.GetUser)
		r.Get("/{username}/repos", h.ListUserRepos)
	})

	// Org routes
	r.Route("/api/orgs", func(r chi.Router) {
		r.Use(optAuthMW)
		r.With(authMW).Post("/", h.CreateOrg)
		r.Get("/{org}", h.GetOrg)
		r.Get("/{org}/members", h.ListOrgMembers)
		r.With(authMW).Post("/{org}/members", h.AddOrgMember)
		r.With(authMW).Delete("/{org}/members/{username}", h.RemoveOrgMember)
		r.With(authMW).Post("/{org}/repos", h.CreateOrgRepo)
		r.With(authMW).Post("/{org}/transfer", h.TransferOrg)
	})

	// Repo routes
	r.Route("/api/repos", func(r chi.Router) {
		r.Use(optAuthMW)
		r.Get("/", h.ListRepos)
		r.With(authMW).Post("/", h.CreateRepo)
		r.Get("/{owner}/{repo}", h.GetRepo)

		// Issues
		r.Route("/{owner}/{repo}/issues", func(r chi.Router) {
			r.Get("/", h.ListIssues)
			r.With(authMW).Post("/", h.CreateIssue)
			r.Get("/{number}", h.GetIssue)
			r.With(authMW).Patch("/{number}", h.UpdateIssue)
			r.Get("/{number}/comments", h.ListIssueComments)
			r.With(authMW).Post("/{number}/comments", h.CreateIssueComment)
			r.With(authMW).Patch("/{number}/comments/{commentID}", h.UpdateComment)
			r.With(authMW).Delete("/{number}/comments/{commentID}", h.DeleteComment)
		})

		// Labels
		r.Route("/{owner}/{repo}/labels", func(r chi.Router) {
			r.Get("/", h.ListLabels)
			r.With(authMW).Post("/", h.CreateLabel)
			r.With(authMW).Delete("/{id}", h.DeleteLabel)
		})

		// Issue labels
		r.With(authMW).Post("/{owner}/{repo}/issues/{number}/labels/{labelID}", h.AddIssueLabel)
		r.With(authMW).Delete("/{owner}/{repo}/issues/{number}/labels/{labelID}", h.RemoveIssueLabel)

		// Pull request labels
		r.With(authMW).Post("/{owner}/{repo}/pulls/{number}/labels/{labelID}", h.AddPullLabel)
		r.With(authMW).Delete("/{owner}/{repo}/pulls/{number}/labels/{labelID}", h.RemovePullLabel)

		// Assignees
		r.With(authMW).Post("/{owner}/{repo}/issues/{number}/assignees", h.AddIssueAssignee)
		r.With(authMW).Delete("/{owner}/{repo}/issues/{number}/assignees", h.RemoveIssueAssignee)
		r.With(authMW).Post("/{owner}/{repo}/pulls/{number}/assignees", h.AddPullAssignee)
		r.With(authMW).Delete("/{owner}/{repo}/pulls/{number}/assignees", h.RemovePullAssignee)

		// Stars
		r.With(authMW).Post("/{owner}/{repo}/star", h.StarRepo)
		r.With(authMW).Delete("/{owner}/{repo}/star", h.UnstarRepo)
		r.With(optAuthMW).Get("/{owner}/{repo}/stargazers", h.ListStargazers)

		// Branches and tags
		r.With(authMW).Post("/{owner}/{repo}/branches", h.CreateBranch)
		r.With(authMW).Delete("/{owner}/{repo}/branches", h.DeleteBranch)
		r.With(authMW).Post("/{owner}/{repo}/tags", h.CreateTag)
		r.With(authMW).Delete("/{owner}/{repo}/tags", h.DeleteTag)

		// Pull requests
		r.Route("/{owner}/{repo}/pulls", func(r chi.Router) {
			r.Get("/", h.ListPulls)
			r.With(authMW).Post("/", h.CreatePull)
			r.Get("/{number}", h.GetPull)
			r.With(authMW).Patch("/{number}", h.UpdatePull)
			r.Get("/{number}/reviews", h.ListReviews)
			r.With(authMW).Post("/{number}/reviews", h.SubmitReview)
			r.Get("/{number}/line_comments", h.ListLineComments)
			r.With(authMW).Post("/{number}/line_comments", h.CreateLineComment)
			// /form must be before /{id} to avoid chi wildcard conflict
			r.With(authMW).Get("/{number}/line_comments/form", h.GetLineCommentForm)
			r.With(authMW).Patch("/{number}/line_comments/{id}", h.UpdateLineComment)
			r.With(authMW).Delete("/{number}/line_comments/{id}", h.DeleteLineComment)
			r.With(authMW).Post("/{number}/line_comments/{id}/apply", h.ApplySuggestion)
		})

		// Webhooks
		r.Route("/{owner}/{repo}/hooks", func(r chi.Router) {
			r.Use(optAuthMW)
			r.Get("/", h.ListWebhooks)
			r.With(authMW).Post("/", h.CreateWebhook)
			r.With(authMW).Delete("/{id}", h.DeleteWebhook)
			r.With(authMW).Get("/{id}/deliveries", h.ListWebhookDeliveries)
		})

		// Collaborators
		r.Route("/{owner}/{repo}/collaborators", func(r chi.Router) {
			r.Use(optAuthMW)
			r.Get("/", h.ListCollaborators)
			r.With(authMW).Post("/", h.AddCollaborator)
			r.With(authMW).Delete("/", h.RemoveCollaborator)
		})

		// Releases — /releases/latest must be before /releases/{id}
		r.Route("/{owner}/{repo}/releases", func(r chi.Router) {
			r.Get("/", h.ListReleases)
			r.With(authMW).Post("/", h.CreateRelease)
			r.Get("/latest", h.GetLatestRelease)
			r.Get("/{id}", h.GetRelease)
			r.With(authMW).Patch("/{id}", h.UpdateRelease)
			r.With(authMW).Delete("/{id}", h.DeleteRelease)
		})

		// Commit statuses
		r.With(authMW).Post("/{owner}/{repo}/statuses/{sha}", h.CreateStatus)
		r.Get("/{owner}/{repo}/statuses/{sha}", h.ListStatuses)
		r.Get("/{owner}/{repo}/commits/{sha}/status", h.GetCombinedStatus)

		// Milestones
		r.Route("/{owner}/{repo}/milestones", func(r chi.Router) {
			r.Get("/", h.ListMilestones)
			r.With(authMW).Post("/", h.CreateMilestone)
			r.Get("/{number}", h.GetMilestone)
			r.With(authMW).Patch("/{number}", h.UpdateMilestone)
			r.With(authMW).Delete("/{number}", h.DeleteMilestone)
		})

		// Milestone sidebar for issues and PRs
		r.With(authMW).Post("/{owner}/{repo}/issues/{number}/milestone", h.SetIssueMilestone)
		r.With(authMW).Post("/{owner}/{repo}/pulls/{number}/milestone", h.SetPullMilestone)

		// Fork
		r.With(authMW).Post("/{owner}/{repo}/fork", h.ForkRepo)

		// Ownership transfer
		r.With(authMW).Post("/{owner}/{repo}/transfer", h.TransferRepo)

		// Deploy keys
		r.Route("/{owner}/{repo}/keys", func(r chi.Router) {
			r.Use(optAuthMW)
			r.Get("/", h.ListDeployKeys)
			r.With(authMW).Post("/", h.AddDeployKey)
			r.With(authMW).Delete("/{id}", h.DeleteDeployKey)
		})

		// Reactions
		r.With(optAuthMW).Get("/{owner}/{repo}/comments/{id}/reactions", h.ListReactions)
		r.With(authMW).Post("/{owner}/{repo}/comments/{id}/reactions", h.ToggleReaction)

		// Branch protections
		r.Route("/{owner}/{repo}/branches/protections", func(r chi.Router) {
			r.Use(optAuthMW)
			r.Get("/", h.ListBranchProtections)
			r.With(authMW).Post("/", h.CreateBranchProtection)
			r.With(authMW).Patch("/{id}", h.UpdateBranchProtection)
			r.With(authMW).Delete("/{id}", h.DeleteBranchProtection)
		})
	})

	// Admin API routes
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(authMW, superadminMW)
		r.Post("/settings", h.UpdateSiteSetting)
		r.Post("/invitations", h.CreateInvitation)
		r.Delete("/invitations/{id}", h.DeleteInvitation)
	})

	// Notification routes
	r.Route("/api/notifications", func(r chi.Router) {
		r.Use(authMW)
		r.Post("/read-all", h.MarkAllNotificationsRead)
		r.Patch("/{id}", h.MarkNotificationRead)
		r.Get("/unread-count", h.GetUnreadCount)
	})

	// HTMX fragment endpoints
	r.Route("/fragments", func(r chi.Router) {
		r.Use(optAuthMW)
		r.Get("/{owner}/{repo}/issues/{number}/comments", h.IssueCommentsFragment)
	})

	// SSH Key routes
	r.Route("/api/user/keys", func(r chi.Router) {
		r.Use(authMW)
		r.Get("/", h.ListSSHKeys)
		r.Post("/", h.AddSSHKey)
		r.Delete("/{id}", h.DeleteSSHKey)
	})

	// Personal Access Token routes
	r.With(authMW).Get("/settings/tokens", h.PageTokens)
	r.Route("/api/user/tokens", func(r chi.Router) {
		r.Use(authMW)
		r.Post("/", h.CreateToken)
		r.Delete("/{id}", h.DeleteToken)
	})

	// Git HTTP Smart Protocol routes
	r.With(optAuthMW).Get("/{owner}/{repo}/info/refs", h.GitInfoRefs)
	r.With(optAuthMW).Post("/{owner}/{repo}/git-upload-pack", h.GitUploadPack)
	r.With(optAuthMW).Post("/{owner}/{repo}/git-receive-pack", h.GitReceivePack)

	// Static file serving
	staticFS, _ := fs.Sub(frontend, "frontend")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Get("/*", fileServer.ServeHTTP)

	return r
}
