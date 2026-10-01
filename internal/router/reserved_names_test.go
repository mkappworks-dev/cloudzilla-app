package router_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// An owner name is the first URL segment, so a route added without reserving
// its segment would let a user or org shadow it. Building the router needs no
// database, so this runs without TEST_DATABASE_DSN.
func TestRouter_TopLevelSegmentsAreReservedOwnerNames(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret},
	}
	h, err := router.New(service.New(store.New(nil), cfg), cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	segments := map[string]bool{}
	err = chi.Walk(h.(chi.Routes), func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seg, _, _ := strings.Cut(strings.TrimPrefix(route, "/"), "/")
		if seg != "" && !strings.ContainsAny(seg, "{}*.") {
			segments[seg] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(segments) < 20 {
		t.Fatalf("found %d static top-level segments; the walk looks broken", len(segments))
	}
	for seg := range segments {
		if service.ValidateOwnerName(seg) == nil {
			t.Errorf("top-level route segment %q is a valid owner name; reserve it", seg)
		}
	}
}
