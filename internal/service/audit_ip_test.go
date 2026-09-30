package service

import (
	"net/http/httptest"
	"testing"
)

// Proxies are resolved by middleware.ClientIP; any client can send the header.
func TestExtractIP_IgnoresForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	req.Header.Set("X-Forwarded-For", "6.6.6.6")

	if got := extractIP(req); got != "203.0.113.9" {
		t.Errorf("want the connection's address, got %s", got)
	}
}
