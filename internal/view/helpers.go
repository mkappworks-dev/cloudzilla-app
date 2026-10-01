package view

import "net/url"

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
