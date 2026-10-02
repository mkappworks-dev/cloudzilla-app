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

// LineCommentID is the DOM id of the inline comment row for k. htmx
// targets it with a "#id" selector, so the path is hashed: a raw '.' or '/'
// breaks the selector.
func LineCommentID(k LineCommentKey) string {
	return "lc-" + k.idSuffix()
}

// LineCommentFormID is the DOM id of the comment-form slot inside that row.
func LineCommentFormID(k LineCommentKey) string {
	return "lc-form-" + k.idSuffix()
}

func (k LineCommentKey) idSuffix() string {
	sum := sha256.Sum256([]byte(k.Path))
	return hex.EncodeToString(sum[:8]) + "-" + k.Side + "-" + strconv.Itoa(k.Line)
}
