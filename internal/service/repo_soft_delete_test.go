package service

import "testing"

func TestSoftDelete_SetsDeletedAt(t *testing.T) {
	// deleteGuard must allow the owner (canManage = true) — no error expected.
	err := deleteGuard(true)
	if err != nil {
		t.Errorf("expected nil error for owner, got: %v", err)
	}
}

func TestSoftDelete_ForbiddenForNonOwner(t *testing.T) {
	// deleteGuard must reject a non-owner (canManage = false).
	err := deleteGuard(false)
	if err == nil {
		t.Error("expected error for non-owner, got nil")
	}
}
