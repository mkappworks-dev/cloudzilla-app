package adminmode_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
)

func TestOverride_OnlyForTheBoundUser(t *testing.T) {
	ctx, _ := adminmode.With(context.Background(), 7, time.Now().Add(time.Hour))

	if !adminmode.Override(ctx, 7) {
		t.Error("the admin-mode user must be overridden")
	}
	if adminmode.Override(ctx, 8) {
		t.Error("another user's check inside the same request must not be widened")
	}
}

func TestOverride_NoStateNeverOverrides(t *testing.T) {
	if adminmode.Override(context.Background(), 7) {
		t.Error("a context without admin mode must not override")
	}
}

func TestUsed_SetOnlyWhenTheOverrideIsAsked(t *testing.T) {
	ctx, st := adminmode.With(context.Background(), 7, time.Now().Add(time.Hour))
	adminmode.Override(ctx, 8)
	if st.Used() {
		t.Error("a refused override for another user must not count as used")
	}
	adminmode.Override(ctx, 7)
	if !st.Used() {
		t.Error("an override for the bound user must be recorded")
	}
}
