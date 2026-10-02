package view

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
)

// EmojiChar converts an emoji shortcode to its Unicode character.
func EmojiChar(emoji string) string {
	m := map[string]string{
		"+1": "👍", "-1": "👎", "laugh": "😄", "hooray": "🎉",
		"confused": "😕", "heart": "❤️", "rocket": "🚀", "eyes": "👀",
	}
	if ch, ok := m[emoji]; ok {
		return ch
	}
	return emoji
}

// Percent returns part*100/total, returning 0 if total is zero.
func Percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}

// WithNext appends next to path as the query parameter the sign-in flow
// returns to once the user is authenticated.
func WithNext(path, next string) string {
	if next == "" {
		return path
	}
	return path + "?next=" + url.QueryEscape(next)
}

// LineCommentID is the DOM id of the inline comment row for path:line. htmx
// targets it with a "#id" selector, so the path is hashed: a raw '.' or '/'
// breaks the selector.
func LineCommentID(path string, line int) string {
	return "lc-" + lineCommentKey(path, line)
}

// LineCommentFormID is the DOM id of the comment-form slot inside that row.
func LineCommentFormID(path string, line int) string {
	return "lc-form-" + lineCommentKey(path, line)
}

func lineCommentKey(path string, line int) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8]) + "-" + strconv.Itoa(line)
}
