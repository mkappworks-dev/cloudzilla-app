package ssh

import (
	"log/slog"
	"runtime/debug"
)

// safeGo runs fn in a new goroutine with panic recovery. A panic in a
// fire-and-forget side-effect (webhook dispatch, post-receive ingestion,
// deploy-key last_used update) must never tear down the SSH server process.
// `label` is a short identifier used in the panic log line.
func safeGo(label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic", "label", label, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
