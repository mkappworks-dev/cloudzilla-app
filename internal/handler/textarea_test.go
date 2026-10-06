package handler

import "testing"

func TestTextareaText(t *testing.T) {
	tests := []struct {
		in   string
		crlf bool
		want string
	}{
		{"a\r\nb\r\n", false, "a\nb\n"},
		{"a\r\nb\r\n", true, "a\r\nb\r\n"},
		{"a\nb", true, "a\r\nb"},
		{"", true, ""},
	}
	for _, tt := range tests {
		if got := textareaText(tt.in, tt.crlf); got != tt.want {
			t.Errorf("textareaText(%q, %v) = %q, want %q", tt.in, tt.crlf, got, tt.want)
		}
	}
	for s, want := range map[string]bool{"a\r\nb\r\n": true, "a\r\nb\n": false, "a": false, "": false} {
		if got := allCRLF(s); got != want {
			t.Errorf("allCRLF(%q) = %v, want %v", s, got, want)
		}
	}
}
