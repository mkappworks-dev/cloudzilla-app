// Package concurrency holds shared goroutine primitives so handler, ssh, and cmd-server can use one implementation.
package concurrency

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine with panic recovery; a panicking fire-and-forget side-effect must not kill the process.
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
