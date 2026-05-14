// Package concurrency holds tiny primitives for launching fire-and-forget
// background work safely. It lives in `internal/` so handlers, SSH session
// handling, and the cmd-server bootstrap can all share one implementation.
package concurrency

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine with panic recovery. A panic in a fire-and-forget
// side-effect (webhook dispatch, post-receive ingestion, search indexing,
// access-token/deploy-key last-used updates, startup backfill) must never tear
// down the server process. `label` is a short identifier used in the panic log.
func Go(label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic", "label", label, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
