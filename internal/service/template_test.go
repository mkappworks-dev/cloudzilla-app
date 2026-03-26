package service

import "testing"

func TestTemplateName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"bug_report.md", "bug report"},
		{"feature-request.md", "feature request"},
		{"Issue.md", "Issue"},
		{"multi-word_name.md", "multi word name"},
		{"no_extension", "no_extension"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := templateName(tt.in); got != tt.want {
				t.Errorf("templateName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
