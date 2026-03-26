package service

import "testing"

func TestValidEmoji(t *testing.T) {
	tests := []struct {
		emoji string
		want  bool
	}{
		{"+1", true},
		{"-1", true},
		{"laugh", true},
		{"hooray", true},
		{"confused", true},
		{"heart", true},
		{"rocket", true},
		{"eyes", true},
		{"fire", false},
		{"", false},
		{"thumbsup", false},
		{":+1:", false},
	}
	for _, tt := range tests {
		t.Run(tt.emoji, func(t *testing.T) {
			if got := validEmoji(tt.emoji); got != tt.want {
				t.Errorf("validEmoji(%q) = %v, want %v", tt.emoji, got, tt.want)
			}
		})
	}
}
