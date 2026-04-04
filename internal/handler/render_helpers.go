package handler

import (
	"github.com/mkappworks/cloudzilla/internal/service"
)

// renderMentionsHTML wraps @username tokens in the already-HTML-rendered string
// with profile anchor tags. Input is trusted HTML from the markdown renderer.
func renderMentionsHTML(html string) string {
	return service.MentionRegex.ReplaceAllStringFunc(html, func(match string) string {
		user := match[1:] // strip leading @
		return `<a href="/` + user + `" class="text-blue-600 hover:underline">` + match + `</a>`
	})
}
