//go:build !dev

package router

import "net/http"

// A release build serves no /_dev/ routes.
func withDevProbes(probes map[string]http.HandlerFunc) map[string]http.HandlerFunc {
	return probes
}
