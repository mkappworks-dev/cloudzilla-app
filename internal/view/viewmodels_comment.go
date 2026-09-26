package view

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

// CommentFragData holds template data for a single comment HTMX fragment.
type CommentFragData struct {
	Comment RenderedComment
}

// CommentsFragData holds template data for the comments list HTMX fragment.
type CommentsFragData struct {
	Comments []RenderedComment
}

// ReactionFragData is used by the reactions fragment.
// ReactionFragData holds template data for the reactions HTMX fragment.
type ReactionFragData struct {
	Owner     string
	RepoName  string
	CommentID int64
	Reactions []model.ReactionSummary
	LoggedIn  bool
}

// Saved replies HTMX list fragment
// SavedRepliesFragData holds template data for the saved replies list HTMX fragment.
type SavedRepliesFragData struct {
	Replies []model.SavedReply
}

// Saved replies picker fragment (for comment textareas)
// SavedRepliesPickerFragData holds template data for the saved replies picker dropdown.
type SavedRepliesPickerFragData struct {
	Replies []model.SavedReply
}
