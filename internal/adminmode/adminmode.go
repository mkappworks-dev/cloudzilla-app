// Package adminmode carries a superadmin's time-boxed admin mode through a
// request context. The state names the one user it widens access for, so a
// permission check run for anyone else in the same request (a notification
// recipient, a webhook target) is unchanged.
package adminmode

import (
	"context"
	"sync/atomic"
	"time"
)

type ctxKey struct{}

// State is the admin mode of one request.
type State struct {
	userID    int64
	expiresAt time.Time
	used      atomic.Bool
}

// With returns ctx carrying admin mode for userID, and the state the audit
// middleware reads once the handler is done.
func With(ctx context.Context, userID int64, expiresAt time.Time) (context.Context, *State) {
	st := &State{userID: userID, expiresAt: expiresAt}
	return context.WithValue(ctx, ctxKey{}, st), st
}

// FromContext returns the request's admin mode, or nil outside it.
func FromContext(ctx context.Context) *State {
	st, _ := ctx.Value(ctxKey{}).(*State)
	return st
}

func (s *State) UserID() int64        { return s.userID }
func (s *State) ExpiresAt() time.Time { return s.expiresAt }

// Used reports whether a permission check passed only through admin mode.
func (s *State) Used() bool { return s.used.Load() }

// Override reports whether admin mode widens access for userID, and records
// that it did. Call it only after the ordinary check has failed, so Used means
// the request would have been refused without admin mode.
func Override(ctx context.Context, userID int64) bool {
	st := FromContext(ctx)
	if st == nil || st.userID != userID {
		return false
	}
	st.used.Store(true)
	return true
}
