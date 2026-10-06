package handler

import "strings"

// textareaText turns the CRLF line breaks a browser submits for a textarea
// back into LF, or keeps CRLF when crlf is set.
func textareaText(s string, crlf bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// allCRLF reports whether every line break in s is CRLF, and there is one.
func allCRLF(s string) bool {
	n := strings.Count(s, "\n")
	return n > 0 && strings.Count(s, "\r\n") == n
}
