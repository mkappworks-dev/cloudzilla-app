package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// Must match static/file_tree.js, which writes the cookies.
const (
	treeOpenCookie     = "cz_tree_open"
	maxOpenTreeFolders = 50
	treeHiddenCookie   = "cz_tree_hidden"
)

func treeHidden(r *http.Request) bool {
	c, err := r.Cookie(treeHiddenCookie)
	return err == nil && c.Value == "1"
}

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
		href := codeurl.Path(owner, repoName, kind, ref, entryPath)
		node := components.TreeNode{
			Name:     e.Name,
			IsDir:    e.IsDir,
			Href:     href,
			Path:     entryPath,
			IsActive: !e.IsDir && len(remainingPath) == 1 && e.Name == remainingPath[0],
		}
		if e.IsDir {
			node.ChildrenURL = "/fragments" + codeurl.Path(owner, repoName, "tree", ref, entryPath)
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

// expandAllBudget bounds Expand all, which costs one tree read per folder.
var expandAllBudget = 300

func expandAllURL(owner, repoName, ref, activePath string) string {
	return "/fragments" + codeurl.Path(owner, repoName, "tree", ref, "") + "/?expand=all&active=" + url.QueryEscape(activePath)
}

// expandAllLevel builds dirPath's entries with folders opened breadth-first,
// so a budget that runs out leaves the deepest folders closed (and lazy).
func (h *Handler) expandAllLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, activePath string) []components.TreeNode {
	nodes := h.buildSidebarLevel(owner, repoName, ref, dirPath, entries, nil, nil)
	var queue []*components.TreeNode
	enqueue := func(level []components.TreeNode) {
		for i := range level {
			level[i].IsActive = !level[i].IsDir && level[i].Path == activePath
			if level[i].IsDir {
				queue = append(queue, &level[i])
			}
		}
	}
	enqueue(nodes)
	for budget := expandAllBudget; len(queue) > 0 && budget > 0; {
		n := queue[0]
		queue = queue[1:]
		child, err := h.Services.Code.GetTree(owner, repoName, ref, n.Path)
		if err != nil {
			slog.Warn("sidebar: expand-all GetTree failed",
				"owner", owner, "repo", repoName, "ref", ref, "path", n.Path, "error", err)
			continue
		}
		budget--
		n.IsOpen = true
		n.Children = h.buildSidebarLevel(owner, repoName, ref, n.Path, child.Entries, nil, nil)
		enqueue(n.Children)
	}
	return nodes
}

// FileTreeChildrenFragment renders a folder's entries for a file-tree folder
// the page rendered closed, or with ?expand=all the whole tree from that
// folder (the root when the path is empty) for the Expand all button.
func (h *Handler) FileTreeChildrenFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	expandAll := r.URL.Query().Get("expand") == "all"
	if path == "" && !expandAll {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	dir, err := h.Services.Code.GetTree(owner, repoName, ref, path)
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	depth := 0
	if path != "" {
		depth = strings.Count(path, "/") + 1
	}
	var nodes []components.TreeNode
	if expandAll {
		nodes = h.expandAllLevel(owner, repoName, ref, path, dir.Entries, r.URL.Query().Get("active"))
	} else {
		nodes = h.buildSidebarLevel(owner, repoName, ref, path, dir.Entries, nil, nil)
	}
	h.render(w, r, fragments.FileTreeChildren(nodes, depth))
}
