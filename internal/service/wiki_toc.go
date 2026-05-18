package service

import (
	"regexp"
	"strings"
)

type WikiTOCEntry struct {
	Level  int
	Anchor string
	Text   string
}

var hRe = regexp.MustCompile(`(?is)<h([1-6])(?:\s+id="([^"]+)")?[^>]*>(.*?)</h[1-6]>`)
var tagRe = regexp.MustCompile(`<[^>]+>`)

func ExtractWikiTOC(html string) []WikiTOCEntry {
	matches := hRe.FindAllStringSubmatch(html, -1)
	out := make([]WikiTOCEntry, 0, len(matches))
	for _, m := range matches {
		level := int(m[1][0] - '0')
		anchor := m[2]
		text := tagRe.ReplaceAllString(m[3], "")
		text = strings.TrimSpace(text)
		if anchor == "" {
			anchor = slugify(text)
		}
		out = append(out, WikiTOCEntry{Level: level, Anchor: anchor, Text: text})
	}
	return out
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
