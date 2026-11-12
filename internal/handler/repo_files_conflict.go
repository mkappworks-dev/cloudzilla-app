package handler

import (
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/textmerge"
)

// editConflict is what a stale edit shows: the text for the textarea, a notice
// and the diff of what changed on the branch.
type editConflict struct {
	Content  string
	Notice   string
	Upstream *service.FileDiff
}

// mergeableText reports whether f is a file the editor could open as text.
func mergeableText(f *service.BranchFile) bool {
	return f != nil && editRefusal(f) == ""
}

// openedText reads the blob the user opened, or nil when it is gone or not text.
func (h *Handler) openedText(owner, repoName, openedSHA string) *service.BranchFile {
	f, err := h.Services.Code.GetBlobByHash(owner, repoName, openedSHA, maxEditFileBytes)
	if err != nil || !mergeableText(f) {
		return nil
	}
	return f
}

// upstreamDiff is the diff from the opened blob to the current file at path, or
// nil when either side isn't text.
func (h *Handler) upstreamDiff(owner, repoName, path, openedSHA string, current *service.BranchFile) *service.FileDiff {
	if !mergeableText(current) {
		return nil
	}
	opened := h.openedText(owner, repoName, openedSHA)
	if opened == nil {
		return nil
	}
	d := h.Services.Code.DiffContent(path, path, opened.Content, current.Content)
	return &d
}

// recoverEdit three-way merges the user's text with the changes made on ref
// since they opened openedSHA. ours is the textarea's text with LF line breaks;
// current is nil when the path holds no file now.
func (h *Handler) recoverEdit(owner, repoName, ref, path, openedSHA, ours string, current *service.BranchFile) editConflict {
	out := editConflict{Content: ours}
	switch {
	case current == nil:
		out.Notice = fmt.Sprintf("%s no longer exists on %s, so there is nothing to merge your changes into. Copy your text before you leave.", path, ref)
		return out
	case !mergeableText(current):
		out.Notice = fmt.Sprintf("%s on %s is no longer text the editor can open, so your changes weren't merged.", path, ref)
		return out
	}
	opened := h.openedText(owner, repoName, openedSHA)
	if opened == nil {
		out.Notice = "Couldn't load the version you opened, so your changes weren't merged."
		return out
	}
	d := h.Services.Code.DiffContent(path, path, opened.Content, current.Content)
	out.Upstream = &d

	merged := textmerge.Merge(lf(string(opened.Content)), ours, lf(string(current.Content)), ref)
	out.Content = merged.Text
	switch merged.Conflicts {
	case 0:
		out.Notice = "Changes made on " + ref + " since you opened this file were merged into your text. Check the diff, then commit."
	case 1:
		out.Notice = "1 conflict needs resolving. " + resolveHint(ref)
	default:
		out.Notice = fmt.Sprintf("%d conflicts need resolving. %s", merged.Conflicts, resolveHint(ref))
	}
	return out
}

func resolveHint(ref string) string {
	return "Edit each block between the “<<<<<<< your changes” and “" + textmerge.TheirsLabel(ref) +
		"” lines, delete the marker lines, then commit."
}

func markerError(ref string) string {
	return "Remove the “<<<<<<< your changes” and “" + textmerge.TheirsLabel(ref) + "” lines before committing."
}

func lf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
