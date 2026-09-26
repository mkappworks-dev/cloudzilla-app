package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// Scope lists are "any one of", narrowest first: the first entry is what an
// insufficient_scope challenge tells the client to request.
var (
	readScopes  = []string{model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite}
	writeScopes = []string{model.ScopeRepoWrite}
	issueScopes = []string{model.ScopeIssuesWrite, model.ScopeRepoWrite}
	pullScopes  = []string{model.ScopePullsWrite, model.ScopeRepoWrite}
)

// Repo sub-resources that administer the repo rather than its content. No scope
// admits them, so a delegated app cannot change who has access or where data flows.
var repoAdminResources = map[string]bool{
	"hooks":         true,
	"collaborators": true,
	"keys":          true,
	"topics":        true,
	"transfer":      true,
	"archive":       true,
	"unarchive":     true,
	"restore":       true,
	"delete":        true,
	"template":      true,
}

// acceptedScopes returns the scopes, any one of which admits a scoped token to r.
// It is an allow-list: a route not matched here returns nil and is closed to
// scoped tokens, which keeps new routes safe by default.
func acceptedScopes(r *http.Request) []string {
	// Split the path chi routes on, so an encoded "%2F" cannot shift segments
	// between what the router matches and what this policy sees.
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	seg := strings.Split(strings.Trim(path, "/"), "/")
	read := r.Method == http.MethodGet || r.Method == http.MethodHead

	if seg[0] == "api" && len(seg) >= 2 {
		switch seg[1] {
		case "repos":
			return repoAPIScopes(seg[2:], read)
		case "orgs":
			if read {
				return readScopes
			}
			if len(seg) == 4 && seg[3] == "repos" {
				return writeScopes
			}
		case "users":
			if read {
				return readScopes
			}
		}
		return nil
	}
	return gitTransportScopes(r, seg)
}

// repoAPIScopes handles /api/repos/{rest...}.
func repoAPIScopes(rest []string, read bool) []string {
	if len(rest) >= 3 && isRepoAdminPath(rest[2:]) {
		return nil
	}
	switch {
	case read:
		return readScopes
	case len(rest) <= 1: // create repo, create from template
		return writeScopes
	case len(rest) == 2: // PATCH repo settings
		return nil
	case rest[2] == "issues":
		return issueScopes
	case rest[2] == "pulls":
		return pullScopes
	}
	return writeScopes
}

func isRepoAdminPath(sub []string) bool {
	return repoAdminResources[sub[0]] || (sub[0] == "branches" && len(sub) >= 2 && sub[1] == "protections")
}

// gitTransportScopes handles /{owner}/{repo}/info/refs, git-upload-pack and git-receive-pack.
func gitTransportScopes(r *http.Request, seg []string) []string {
	switch {
	case len(seg) == 4 && seg[2] == "info" && seg[3] == "refs":
		if r.URL.Query().Get("service") == "git-receive-pack" {
			return writeScopes
		}
		return readScopes
	case len(seg) == 3 && seg[2] == "git-upload-pack":
		return readScopes
	case len(seg) == 3 && seg[2] == "git-receive-pack":
		return writeScopes
	}
	return nil
}

// scopeAllows reports whether c may make request r.
func scopeAllows(c Claims, r *http.Request) bool {
	return !c.Scoped || slices.ContainsFunc(acceptedScopes(r), c.HasScope)
}

// writeInsufficientScope answers a scoped token that lacks the scope for r (RFC 6750 §3.1).
func writeInsufficientScope(w http.ResponseWriter, r *http.Request) {
	challenge := `Bearer error="insufficient_scope"`
	if accepted := acceptedScopes(r); len(accepted) > 0 {
		challenge += `, scope="` + accepted[0] + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"insufficient_scope"}`))
}
