// Package codeurl builds code-browser URLs from git refs and repo paths.
package codeurl

import (
	"net/url"
	"strings"
)

// Escape path-escapes each "/"-separated segment, keeping the slashes as separators.
func Escape(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// Path returns /{owner}/{repo}/{kind}/{ref}[/{path}] with ref and path escaped.
func Path(owner, repo, kind, ref, path string) string {
	u := "/" + owner + "/" + repo + "/" + kind + "/" + Escape(ref)
	if path != "" {
		u += "/" + Escape(path)
	}
	return u
}
