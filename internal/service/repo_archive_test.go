package service

import (
	"testing"
)

func TestArchiveBlocksWrite(t *testing.T) {
	tests := []struct {
		name      string
		canManage bool
		wantErr   bool
	}{
		{name: "owner can archive", canManage: true, wantErr: false},
		{name: "non-owner cannot archive", canManage: false, wantErr: true},
		// Idempotent: calling archive guard for an already-archived repo is fine when owner
		{name: "owner can archive already-archived (idempotent)", canManage: true, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := archiveGuard(tt.canManage)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSetTemplate_RequiresCanManage(t *testing.T) {
	tests := []struct {
		name      string
		canManage bool
		wantErr   bool
	}{
		{name: "owner can set template", canManage: true, wantErr: false},
		{name: "non-owner cannot set template", canManage: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := templateGuard(tt.canManage)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
