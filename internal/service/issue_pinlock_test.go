package service

import (
	"testing"
)

func TestPinLimitGuard(t *testing.T) {
	tests := []struct {
		name        string
		pinnedCount int
		wantErr     string
	}{
		{name: "zero pinned", pinnedCount: 0, wantErr: ""},
		{name: "one pinned", pinnedCount: 1, wantErr: ""},
		{name: "two pinned", pinnedCount: 2, wantErr: ""},
		{name: "three pinned — at limit", pinnedCount: 3, wantErr: ""},
		{name: "four pinned — exceeds limit", pinnedCount: 4, wantErr: "repositories may not have more than 3 pinned issues"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pinLimitGuard(tt.pinnedCount)
			if got != tt.wantErr {
				t.Errorf("pinLimitGuard(%d) = %q, want %q", tt.pinnedCount, got, tt.wantErr)
			}
		})
	}
}

func TestLockGuard(t *testing.T) {
	tests := []struct {
		name     string
		isLocked bool
		wantErr  string
	}{
		{name: "unlocked issue allows comment", isLocked: false, wantErr: ""},
		{name: "locked issue blocks comment", isLocked: true, wantErr: "issue is locked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lockGuard(tt.isLocked)
			if got != tt.wantErr {
				t.Errorf("lockGuard(%v) = %q, want %q", tt.isLocked, got, tt.wantErr)
			}
		})
	}
}
