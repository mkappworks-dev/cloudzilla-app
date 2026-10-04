package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// Must match static/file_tree.js, which writes the cookie.
const (
	treeOpenCookie     = "cz_tree_open"
	maxOpenTreeFolders = 50
)

// openTreeFolders returns the folders the viewer left open in the file tree.
// The cookie is client-written, so anything unparseable counts as none.
func openTreeFolders(r *http.Request) map[string]bool {
	c, err := r.Cookie(treeOpenCookie)
	if err != nil {
		return nil
	}
	raw, err := url.PathUnescape(c.Value)
	if err != nil {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil
	}
	if len(paths) > maxOpenTreeFolders {
		paths = paths[:maxOpenTreeFolders]
	}
	open := make(map[string]bool, len(paths))
	for _, p := range paths {
		open[p] = true
	}
	return open
}

// buildSidebarTree returns the root tree with the folders along path and the
// open folders expanded and, when path is a file, that file marked active.
// Pass the requested ref, not a result's display Ref, which shortens a SHA
// past resolving.
func (h *Handler) buildSidebarTree(owner, repoName, ref, path string, open map[string]bool) []components.TreeNode {
	root, err := h.Services.Code.GetTree(owner, repoName, ref, "")
	if err != nil {
		slog.Warn("sidebar: root GetTree failed",
			"owner", owner, "repo", repoName, "ref", ref, "error", err)
		return nil
	}
	var segs []string
	if path != "" {
		segs = strings.Split(path, "/")
	}
	return h.buildSidebarLevel(owner, repoName, ref, "", root.Entries, segs, open)
}

// buildSidebarLevel expands the directory matching remainingPath[0] and every
// directory in open, recursively.
func (h *Handler) buildSidebarLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, remainingPath []string, open map[string]bool) []components.TreeNode {
	nodes := make([]components.TreeNode, 0, len(entries))
	for _, e := range entries {
		var entryPath string
		if dirPath == "" {
			entryPath = e.Name
		} else {
			entryPath = dirPath + "/" + e.Name
		}
		kind := "tree"
		if !e.IsDir {
			kind = "blob"
		}
		href := "/" + owner + "/" + repoName + "/" + kind + "/" + ref + "/" + entryPath
		node := components.TreeNode{
			Name:     e.Name,
			IsDir:    e.IsDir,
			Href:     href,
			Path:     entryPath,
			IsActive: !e.IsDir && len(remainingPath) == 1 && e.Name == remainingPath[0],
		}
		if e.IsDir {
			node.ChildrenURL = "/fragments/" + owner + "/" + repoName + "/tree/" + ref + "/" + entryPath
		}
		onPath := len(remainingPath) > 0 && e.Name == remainingPath[0]
		if e.IsDir && (onPath || open[entryPath]) {
			node.IsOpen = true
			var rest []string
			if onPath {
				rest = remainingPath[1:]
			}
			child, err := h.Services.Code.GetTree(owner, repoName, ref, entryPath)
			if err != nil {
				slog.Warn("sidebar: child GetTree failed",
					"owner", owner, "repo", repoName, "ref", ref, "path", entryPath, "error", err)
			} else {
				node.Children = h.buildSidebarLevel(owner, repoName, ref, entryPath, child.Entries, rest, open)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// FileTreeChildrenFragment renders a folder's entries for a file-tree folder
// the page rendered closed.
func (h *Handler) FileTreeChildrenFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "*")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	if path == "" {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	dir, err := h.Services.Code.GetTree(owner, repoName, ref, path)
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	nodes := h.buildSidebarLevel(owner, repoName, ref, path, dir.Entries, nil, nil)
	h.render(w, r, fragments.FileTreeChildren(nodes, strings.Count(path, "/")+1))
}
