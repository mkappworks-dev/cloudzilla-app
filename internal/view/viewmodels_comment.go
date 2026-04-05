package view

import "github.com/mkappworks/cloudzilla/internal/model"

type CommentFragData struct {
	Comment RenderedComment
}

type CommentsFragData struct {
	Comments []RenderedComment
}

// ReactionFragData is used by the reactions fragment.
type ReactionFragData struct {
	Owner     string
	RepoName  string
	CommentID int64
	Reactions []model.ReactionSummary
	LoggedIn  bool
}

// Saved replies settings page
type SavedRepliesData struct {
	BasePage
	Replies []model.SavedReply
}

// Saved replies HTMX list fragment
type SavedRepliesFragData struct {
	Replies []model.SavedReply
}

// Saved replies picker fragment (for comment textareas)
type SavedRepliesPickerFragData struct {
	Replies []model.SavedReply
}
