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
	adminScopes = []string{model.ScopeRepoAdmin}
)

// Sub-resources of /api/repos/{owner}/{repo} that administer it. Only a personal
// access token with repo:admin reaches them; OAuth apps can't be granted it.
var repoAdminResources = map[string]bool{
	"collaborators": true,
	"keys":          true,
	"hooks":         true,
	"transfer":      true,
	"delete":        true,
}

// Sub-resources of /api/repos/{owner}/{repo} that hold repository content.
// Everything else there administers the repo (hooks, collaborators, keys,
// topics, transfer, delete, …) and stays closed to scoped tokens.
var repoContentResources = map[string]bool{
	"issues":      true,
	"pulls":       true,
	"labels":      true,
	"milestones":  true,
	"releases":    true,
	"statuses":    true,
	"commits":     true,
	"branches":    true,
	"tags":        true,
	"comments":    true,
	"stargazers":  true,
	"star":        true,
	"watch":       true,
	"fork":        true,
	"projects":    true,
	"wiki":        true,
	"discussions": true,
}

// pathSegments splits the path chi routes on, so an encoded "%2F" cannot shift
// segments between what the router matches and what a policy here sees.
func pathSegments(r *http.Request) []string {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	return strings.Split(strings.Trim(path, "/"), "/")
}

// TargetAllows reports whether r stays within targets, the repositories
// ("owner/repo") and organizations ("org") a token is limited to. An
// organization covers its own routes and every repository it owns. A request
// that names neither is left to the token's scopes.
func TargetAllows(targets []string, r *http.Request) bool {
	seg := pathSegments(r)
	switch {
	case len(seg) >= 4 && seg[0] == "api" && seg[1] == "repos":
		return model.TargetsCover(targets, seg[2], seg[3])
	case len(seg) >= 3 && seg[0] == "api" && seg[1] == "orgs":
		return slices.ContainsFunc(targets, func(t string) bool {
			return !strings.Contains(t, "/") && strings.EqualFold(t, seg[2])
		})
	default:
		return true
	}
}

// acceptedScopes returns the scopes, any one of which admits a scoped token to r.
// Only routes matched here are open to scoped tokens; nil means closed.
func acceptedScopes(r *http.Request) []string {
	seg := pathSegments(r)
	read := r.Method == http.MethodGet || r.Method == http.MethodHead

	if seg[0] == "api" && len(seg) >= 2 {
		switch seg[1] {
		case "repos":
			return repoAPIScopes(seg[2:], read)
		case "orgs": // /api/orgs/{org}, /members, /repos
			switch {
			case read && (len(seg) == 3 || len(seg) == 4 && seg[3] == "members"):
				return readScopes
			case !read && len(seg) == 4 && seg[3] == "repos":
				return writeScopes
			case !read && len(seg) >= 4 && (seg[3] == "members" || seg[3] == "transfer" || seg[3] == "delete"):
				return adminScopes
			}
		case "users": // /api/users/{username}, /repos
			if read && (len(seg) == 3 || len(seg) == 4 && seg[3] == "repos") {
				return readScopes
			}
		}
		return nil
	}
	return gitTransportScopes(r, seg)
}

// repoAPIScopes handles /api/repos/{rest...}.
func repoAPIScopes(rest []string, read bool) []string {
	switch {
	case len(rest) == 0: // list or create repos
		if read {
			return readScopes
		}
		return writeScopes
	case len(rest) == 1:
		if !read && rest[0] == "from-template" {
			return writeScopes
		}
		return nil
	case len(rest) == 2: // the repo itself; PATCH changes its settings
		if read {
			return readScopes
		}
		return nil
	}

	sub := rest[2:]
	if repoAdminResources[sub[0]] {
		return adminScopes
	}
	if !repoContentResources[sub[0]] || sub[0] == "branches" && len(sub) >= 2 && sub[1] == "protections" {
		return nil
	}
	switch {
	case read:
		return readScopes
	case sub[0] == "issues":
		return issueScopes
	case sub[0] == "pulls" && len(sub) == 5 && sub[2] == "line_comments" && sub[4] == "apply":
		return writeScopes // applying a suggestion commits to the head branch
	case sub[0] == "pulls":
		return pullScopes
	}
	return writeScopes
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

// ScopeAllows reports whether c may make r: always for unscoped claims, otherwise
// only when one of c's scopes admits r.
func ScopeAllows(c Claims, r *http.Request) bool {
	return !c.Scoped || slices.ContainsFunc(acceptedScopes(r), c.HasScope)
}

// RequiredScope returns the narrowest scope that admits r, or "" when no scope does.
func RequiredScope(r *http.Request) string {
	if accepted := acceptedScopes(r); len(accepted) > 0 {
		return accepted[0]
	}
	return ""
}

// WriteInsufficientScope refuses a scoped token (RFC 6750 §3.1). scope names the
// scope to request, or is "" when no scope would admit the request.
func WriteInsufficientScope(w http.ResponseWriter, scope string) {
	challenge := `Bearer error="insufficient_scope"`
	if scope != "" {
		challenge += `, scope="` + scope + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"insufficient_scope"}`))
}
