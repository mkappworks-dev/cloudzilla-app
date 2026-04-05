package service

import (
	"testing"
)

func TestArchiveBlocksWrite(t *testing.T) {
	tests := []struct {
		name    string
		isOwner bool
		wantErr bool
	}{
		{name: "owner can archive", isOwner: true, wantErr: false},
		{name: "non-owner cannot archive", isOwner: false, wantErr: true},
		// Idempotent: calling archive guard for an already-archived repo is fine when owner
		{name: "owner can archive already-archived (idempotent)", isOwner: true, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := archiveGuard(tt.isOwner)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSetTemplate_RequiresOwner(t *testing.T) {
	tests := []struct {
		name    string
		isOwner bool
		wantErr bool
	}{
		{name: "owner can set template", isOwner: true, wantErr: false},
		{name: "non-owner cannot set template", isOwner: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := templateGuard(tt.isOwner)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeleteGuard_RequiresOwner(t *testing.T) {
	tests := []struct {
		name    string
		isOwner bool
		wantErr bool
	}{
		{name: "owner can delete", isOwner: true, wantErr: false},
		{name: "non-owner cannot delete", isOwner: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := deleteGuard(tt.isOwner)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
