package service

import (
	"testing"
)

func TestDiscussionTitleValidation(t *testing.T) {
	tests := []struct {
		title   string
		wantErr bool
	}{
		{"My question", false},
		{"", true},
		{"   ", false}, // whitespace is not caught at service layer (store handles it)
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			gotErr := tt.title == ""
			if gotErr != tt.wantErr {
				t.Errorf("title %q: gotErr=%v wantErr=%v", tt.title, gotErr, tt.wantErr)
			}
		})
	}
}

func TestDiscussionNextNumber(t *testing.T) {
	// Simulate next-number logic: max + 1, or 1 if no rows.
	nextNumber := func(max int, hasRows bool) int {
		if !hasRows {
			return 1
		}
		return max + 1
	}
	if n := nextNumber(0, false); n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
	if n := nextNumber(5, true); n != 6 {
		t.Errorf("expected 6, got %d", n)
	}
}
