package service

import "regexp"

// MentionRegex matches @username tokens.
var MentionRegex = regexp.MustCompile(`@([A-Za-z0-9_-]+)`)

// parseMentions returns unique usernames found in body (without the @ prefix).
func parseMentions(body string) []string {
	matches := MentionRegex.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		u := m[1]
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}
