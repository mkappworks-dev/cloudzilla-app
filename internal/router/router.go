package router

import (
	"html/template"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/handler"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/service"
)

func mustParseTemplates(frontend fs.FS) (map[string]*template.Template, *template.Template) {
	sub, _ := fs.Sub(frontend, "frontend/templates")
	base := template.Must(template.ParseFS(sub, "layout.html"))

	pageNames := []string{"home", "login", "user", "repo", "issues",
		"issue_detail", "pulls", "pull_detail", "settings"}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		clone := template.Must(base.Clone())
		template.Must(clone.ParseFS(sub, "pages/"+name+".html"))
		pages[name] = clone
	}
	frags := template.Must(template.New("frags").ParseFS(sub, "fragments/*.html"))
	return pages, frags
}

func New(services *service.Services, cfg *config.Config, frontend fs.FS) http.Handler {
	r := chi.NewRouter()
	pages, frags := mustParseTemplates(frontend)
	h := handler.New(services, cfg, pages, frags)

	// Global middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(true))

	authMW := middleware.Auth(cfg.Auth.JWTSecret, cfg.Auth.CookieName)
	optAuthMW := middleware.OptionalAuth(cfg.Auth.JWTSecret, cfg.Auth.CookieName)

	// Page routes
	r.Get("/", h.PageHome)
	r.Get("/login", h.PageLogin)
	r.Post("/login", h.PageLoginSubmit)
	r.With(authMW).Get("/settings", h.PageSettings)
	r.With(optAuthMW).Get("/{owner}", h.PageUser)
	r.With(optAuthMW).Get("/{owner}/{repo}", h.PageRepo)
	r.With(optAuthMW).Get("/{owner}/{repo}/issues", h.PageIssues)
	r.With(optAuthMW).Get("/{owner}/{repo}/issues/{number}", h.PageIssueDetail)
	r.With(optAuthMW).Get("/{owner}/{repo}/pulls", h.PagePulls)
	r.With(optAuthMW).Get("/{owner}/{repo}/pulls/{number}", h.PagePullDetail)

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
			r.With(authMW).Delete("/{number}/comments/{commentID}", h.DeleteComment)
		})

		// Pull requests
		r.Route("/{owner}/{repo}/pulls", func(r chi.Router) {
			r.Get("/", h.ListPulls)
			r.With(authMW).Post("/", h.CreatePull)
			r.Get("/{number}", h.GetPull)
			r.With(authMW).Patch("/{number}", h.UpdatePull)
		})
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
