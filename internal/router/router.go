package router

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// New registers all application routes and returns the configured chi router.
func New(services *service.Services, cfg *config.Config, frontend fs.FS) http.Handler {
	r := chi.NewRouter()
	h := handler.New(services, cfg)

	// Global middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(cfg.Server.BaseURL))
	r.Use(middleware.CSRF(cfg.Auth.CookieSecure))
	r.Use(middleware.RequireSetup(services.SiteSetting))

	authMW := middleware.Auth(cfg.Auth.JWTSecret, cfg.Auth.CookieName, services.AccessToken, services.OAuthApp, h.Unauthorized)
	optAuthMW := middleware.OptionalAuth(cfg.Auth.JWTSecret, cfg.Auth.CookieName, services.AccessToken, services.OAuthApp)
	apiBodyLimit := middleware.MaxBodySize(1 << 20) // 1 MB

	superadminMW := middleware.RequireSuperadmin(h.Forbidden)

	// Custom 404 handler — branded HTML page for site requests, JSON for API.
	r.NotFound(h.NotFound)

	// Setup route (first-run wizard)
	r.Get("/setup", h.PageSetup)
	r.Post("/setup", h.PageSetupSubmit)

	// Invite routes
	r.Get("/invite/{token}", h.PageInvite)
	r.Post("/invite/{token}", h.PageInviteSubmit)

	// Admin routes
	r.With(authMW, superadminMW).Get("/admin/settings", h.PageAdminSettings)
	r.With(authMW, superadminMW).Get("/admin/audit-log", h.PageAuditLog)
	r.With(authMW, superadminMW).Get("/admin/sso", h.PageSSOSettings)
	r.With(authMW, superadminMW).Post("/admin/sso", h.SaveSSOConfig)

	// Search
	r.With(optAuthMW).Get("/search", h.PageSearch)
	r.With(optAuthMW).Get("/search/code", h.PageCodeSearch)

	// Page routes
	r.With(optAuthMW).Get("/", h.PageHome)
	r.With(optAuthMW).Get("/explore", h.PageExplore)
	r.With(optAuthMW).Get("/login", h.PageLogin)
	r.With(optAuthMW).Get("/register", h.PageRegister)
	r.With(optAuthMW).Post("/register", h.PageRegisterSubmit)
	r.With(optAuthMW).Post("/login", h.PageLoginSubmit)
	r.With(authMW).Get("/new", h.PageNewRepo)
	r.With(authMW).Get("/settings", h.PageSettings)
	r.With(authMW).Get("/settings/notifications", h.PageNotificationSettings)
	r.With(authMW).Post("/settings/notifications", h.UpdateNotificationSettings)
	r.With(authMW).Get("/settings/oauth-apps", h.PageOAuthApps)
	r.With(authMW).Get("/notifications", h.PageNotifications)
	r.With(authMW).Get("/feed", h.PageFeed)

	// OAuth 2.0 authorization code flow
	r.With(optAuthMW).Get("/oauth/authorize", h.PageOAuthAuthorize)
	r.With(authMW).Post("/oauth/authorize", h.ConfirmAuthorize)
	r.Post("/oauth/token", h.TokenEndpoint)

	// Gist page routes
	r.With(optAuthMW).Get("/gists", h.PageGists)
	r.With(authMW).Get("/gists/new", h.PageGistNew)
	r.With(optAuthMW).Get("/gists/{id}", h.PageGistDetail)
	r.With(authMW).Get("/gists/{id}/edit", h.PageGistEdit)

	// Gist API routes
	r.Route("/api/gists", func(r chi.Router) {
		r.Use(apiBodyLimit)
		r.With(authMW).Post("/", h.CreateGist)
		r.With(authMW).Get("/file-row", h.AddFileFragment)
		r.With(authMW).Patch("/{id}", h.UpdateGist)
		r.With(authMW).Delete("/{id}", h.DeleteGist)
	})

	// Topic explore page
	r.With(optAuthMW).Get("/topic/{name}", h.PageTopic)

	r.With(optAuthMW).Get("/{owner}", h.PageUser)
	r.With(authMW).Get("/orgs/{org}/settings", h.PageOrgSettings)
	r.With(optAuthMW).Get("/{owner}/gists", h.PageUserGists)
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
	r.With(optAuthMW).Get("/{owner}/{repo}/tree/{ref}/*", h.PageTree)
	r.With(optAuthMW).Get("/{owner}/{repo}/blob/{ref}/*", h.PageBlob)
	r.With(optAuthMW).Get("/{owner}/{repo}/blame/{ref}/*", h.PageBlame)
	r.With(optAuthMW).Get("/{owner}/{repo}/commits", h.PageCommitsRedirect)
	r.With(optAuthMW).Get("/{owner}/{repo}/commits/{ref}", h.PageCommits)
	r.With(optAuthMW).Get("/{owner}/{repo}/commits/{ref}/*", h.PageCommits)
	r.With(optAuthMW).Get("/{owner}/{repo}/commit/{sha}", h.PageCommit)
	r.With(optAuthMW).Get("/{owner}/{repo}/pulse", h.PagePulse)
	r.With(optAuthMW).Get("/{owner}/{repo}/graphs/contributors", h.PageContributors)
	r.With(optAuthMW).Get("/{owner}/{repo}/network/dependencies", h.PageDependencies)
	r.With(optAuthMW).Get("/{owner}/{repo}/wiki", h.PageWikiHome)
	r.With(optAuthMW).Get("/{owner}/{repo}/wiki/{slug}", h.PageWikiPage)
	r.With(authMW).Get("/{owner}/{repo}/wiki/{slug}/edit", h.PageWikiEdit)
	r.With(optAuthMW).Get("/{owner}/{repo}/projects", h.PageProjects)
	r.With(optAuthMW).Get("/{owner}/{repo}/projects/{id}", h.PageProjectDetail)
	r.With(optAuthMW).Get("/{owner}/{repo}/discussions", h.PageDiscussions)
	r.With(optAuthMW).Get("/{owner}/{repo}/discussions/{number}", h.PageDiscussionDetail)

	// OAuth routes
	r.Get("/auth/google", h.GoogleOAuthBegin)
	r.Get("/auth/google/callback", h.GoogleOAuthCallback)

	// SSO auth endpoints
	r.Post("/auth/ldap", h.LDAPLogin)
	r.Get("/auth/saml", h.InitiateSAML)
	r.Post("/auth/saml/callback", h.SAMLCallback)
	r.Get("/auth/saml/metadata", h.SAMLMetadata)

	// Auth routes
	r.Route("/api/auth", func(r chi.Router) {
		r.Use(apiBodyLimit)
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
		r.Use(optAuthMW, apiBodyLimit)
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
		r.Use(optAuthMW, apiBodyLimit)
		r.Get("/", h.ListRepos)
		r.With(authMW).Post("/", h.CreateRepo)
		r.Get("/{owner}/{repo}", h.GetRepo)

		// Issues
		r.Route("/{owner}/{repo}/issues", func(r chi.Router) {
			r.Get("/", h.ListIssues)
			r.With(authMW).Post("/", h.CreateIssue)
			r.Get("/{number}", h.GetIssue)
			r.With(authMW).Patch("/{number}", h.UpdateIssue)
			r.With(authMW).Patch("/{number}/pin", h.PinIssue)
			r.With(authMW).Patch("/{number}/lock", h.LockIssue)
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

		// Watch
		r.With(authMW).Put("/{owner}/{repo}/watch", h.WatchRepo)
		r.With(authMW).Delete("/{owner}/{repo}/watch", h.UnwatchRepo)
		r.With(optAuthMW).Get("/{owner}/{repo}/watch", h.GetWatchButton)

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
			r.With(authMW).Patch("/{id}", h.UpdateWebhook)
			r.With(authMW).Post("/{id}/redeliver", h.RedeliverWebhook)
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

		// Soft-delete restore
		r.With(authMW).Post("/{owner}/{repo}/restore", h.RestoreRepo)

		// Archive / unarchive
		r.With(authMW).Post("/{owner}/{repo}/archive", h.ArchiveRepo)
		r.With(authMW).Post("/{owner}/{repo}/unarchive", h.UnarchiveRepo)

		// Template
		r.With(authMW).Patch("/{owner}/{repo}/template", h.SetRepoTemplate)

		// Create from template
		r.With(authMW).Post("/from-template", h.CreateFromTemplate)

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

		// Projects
		r.Route("/{owner}/{repo}/projects", func(r chi.Router) {
			r.With(authMW).Post("/", h.CreateProject)
			r.With(authMW).Delete("/{id}", h.DeleteProject)
			r.With(authMW).Post("/{id}/columns", h.CreateColumn)
			r.With(authMW).Delete("/{id}/columns/{colID}", h.DeleteColumn)
			r.With(authMW).Post("/{id}/cards", h.CreateCard)
			r.With(authMW).Patch("/{id}/cards/{cardID}", h.MoveCard)
			r.With(authMW).Delete("/{id}/cards/{cardID}", h.DeleteCard)
		})

		// Wiki
		r.With(authMW).Post("/{owner}/{repo}/wiki/{slug}", h.CreateOrUpdateWikiPage)
		r.With(authMW).Delete("/{owner}/{repo}/wiki/{slug}", h.DeleteWikiPage)

		// Topics
		r.With(authMW).Put("/{owner}/{repo}/topics", h.SetTopics)
		r.With(optAuthMW).Get("/{owner}/{repo}/topics", h.GetTopicsFragment)

		// Discussions
		r.Route("/{owner}/{repo}/discussions", func(r chi.Router) {
			r.With(authMW).Post("/", h.CreateDiscussion)
			r.With(authMW).Post("/{number}/replies", h.CreateReply)
			r.With(authMW).Patch("/{number}", h.MarkAnswer)
			r.With(authMW).Delete("/{number}/replies/{id}", h.DeleteDiscussionReply)
			r.With(authMW).Post("/categories", h.CreateDiscussionCategory)
			r.With(authMW).Delete("/categories/{id}", h.DeleteDiscussionCategory)
		})
	})

	// Admin API routes
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(authMW, superadminMW, apiBodyLimit)
		r.Post("/settings", h.UpdateSiteSetting)
		r.Post("/invitations", h.CreateInvitation)
		r.Delete("/invitations/{id}", h.DeleteInvitation)
	})

	// Notification routes
	r.Route("/api/notifications", func(r chi.Router) {
		r.Use(authMW, apiBodyLimit)
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
		r.Use(authMW, apiBodyLimit)
		r.Get("/", h.ListSSHKeys)
		r.Post("/", h.AddSSHKey)
		r.Delete("/{id}", h.DeleteSSHKey)
	})

	// Personal Access Token routes
	r.With(authMW).Get("/settings/tokens", h.PageTokens)
	r.Route("/api/user/tokens", func(r chi.Router) {
		r.Use(authMW, apiBodyLimit)
		r.Post("/", h.CreateToken)
		r.Delete("/{id}", h.DeleteToken)
	})

	// OAuth app API routes
	r.Route("/api/oauth", func(r chi.Router) {
		r.Use(authMW, apiBodyLimit)
		r.Post("/apps", h.CreateOAuthApp)
		r.Delete("/apps/{id}", h.DeleteOAuthApp)
		r.Delete("/authorizations/{id}", h.RevokeOAuthAuthorization)
	})

	// Saved replies routes
	r.With(authMW).Get("/settings/replies", h.PageSavedReplies)
	r.Route("/api/user/replies", func(r chi.Router) {
		r.Use(authMW, apiBodyLimit)
		r.Get("/", h.ListSavedRepliesFragment)
		r.Post("/", h.CreateSavedReply)
		r.Patch("/{id}", h.UpdateSavedReply)
		r.Delete("/{id}", h.DeleteSavedReply)
	})

	// Security / TOTP routes
	r.With(authMW).Get("/settings/security", h.PageSecuritySettings)
	r.With(authMW).Post("/settings/security/setup", h.PageSecuritySettingsSetup)
	r.With(authMW).Post("/api/user/totp/enable", h.EnableTOTP)
	r.With(authMW).Post("/api/user/totp/disable", h.DisableTOTP)

	// TOTP verification (no auth — reads cz_totp_pending cookie)
	r.Get("/auth/2fa", h.PageTOTPVerify)
	r.Post("/auth/2fa/verify", h.VerifyTOTP)

	// Git HTTP Smart Protocol routes
	r.With(optAuthMW).Get("/{owner}/{repo}/info/refs", h.GitInfoRefs)
	r.With(optAuthMW).Post("/{owner}/{repo}/git-upload-pack", h.GitUploadPack)
	r.With(optAuthMW).Post("/{owner}/{repo}/git-receive-pack", h.GitReceivePack)

	// Static file serving — registered on explicit prefixes so chi's radix
	// tree prefers these over the parameterized /{owner}/{repo} routes.
	staticFS, _ := fs.Sub(frontend, "frontend")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Handle("/static/*", fileServer)
	r.Handle("/htmx.min.js", fileServer)
	r.Handle("/alpine.min.js", fileServer)

	// Serve the SVG favicon for the legacy /favicon.ico path that some browsers
	// and bots auto-request even when <link rel="icon"> is declared.
	faviconBytes, _ := fs.ReadFile(staticFS, "static/favicon.svg")
	r.Get("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(faviconBytes)
	})

	return r
}
