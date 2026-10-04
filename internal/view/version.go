package view

import "sync/atomic"

var buildVersion atomic.Pointer[string]

// SetVersion sets the build version the footer shows.
func SetVersion(v string) { buildVersion.Store(&v) }

// Version returns the build version, or "dev" when SetVersion was never called.
func Version() string {
	if v := buildVersion.Load(); v != nil {
		return *v
	}
	return "dev"
}
