package model

// Token scopes. The repo scopes share their names with the personal access
// token form so both credential types speak one vocabulary.
const (
	ScopeRepoRead    = "repo:read"
	ScopeRepoWrite   = "repo:write"
	ScopeIssuesWrite = "issues:write"
	ScopePullsWrite  = "pulls:write"
)

// Scopes lists every grantable scope in the order forms offer them.
var Scopes = []string{ScopeRepoRead, ScopeRepoWrite, ScopeIssuesWrite, ScopePullsWrite}

var scopeDescriptions = map[string]string{
	ScopeRepoRead:    "Read repositories, issues, pull requests and releases you can access",
	ScopeRepoWrite:   "Create and update repository content: issues, pull requests, releases, branches, pushes",
	ScopeIssuesWrite: "Create and update issues and issue comments",
	ScopePullsWrite:  "Create and update pull requests, reviews and comments",
}

// Registry scopes. They are not in Scopes, which is also the OAuth consent list: only personal access tokens and
// device logins can hold them.
const (
	ScopePackagesRead  = "packages:read"
	ScopePackagesWrite = "packages:write"
)

// PackageScopes lists the registry scopes in the order forms offer them.
var PackageScopes = []string{ScopePackagesRead, ScopePackagesWrite}

var packageScopeDescriptions = map[string]string{
	ScopePackagesRead:  "Pull container images and download package files from repositories you can read",
	ScopePackagesWrite: "Push and delete container images and package files in repositories you can write to",
}

// ScopeDescription returns the consent-screen text for scope, or "" if the scope is unknown.
func ScopeDescription(scope string) string {
	if d, ok := scopeDescriptions[scope]; ok {
		return d
	}
	return packageScopeDescriptions[scope]
}

// ScopeRepoAdmin lets a personal access token administer repositories and
// organizations without a confirmation prompt. OAuth apps can't be granted it.
const ScopeRepoAdmin = "repo:admin"

// IsKnownScope reports whether scope can be granted to a token.
func IsKnownScope(scope string) bool {
	_, ok := scopeDescriptions[scope]
	return ok
}

// IsDeviceScope reports whether scope can be requested by a device login.
func IsDeviceScope(scope string) bool {
	_, pkg := packageScopeDescriptions[scope]
	return IsKnownScope(scope) || pkg
}

// IsTokenScope reports whether scope can be given to a personal access token.
func IsTokenScope(scope string) bool {
	return IsDeviceScope(scope) || scope == ScopeRepoAdmin
}
