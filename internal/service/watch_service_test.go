package service

import (
	"testing"
)

func TestWatchLevelValidation(t *testing.T) {
	tests := []struct {
		level   string
		wantErr bool
	}{
		{"watching", false},
		{"releases_only", false},
		{"ignoring", false},
		{"all", true},
		{"", true},
		{"WATCHING", true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			valid := tt.level == "watching" || tt.level == "releases_only" || tt.level == "ignoring"
			if valid == tt.wantErr {
				t.Errorf("level %q: valid=%v but wantErr=%v", tt.level, valid, tt.wantErr)
			}
		})
	}
}
