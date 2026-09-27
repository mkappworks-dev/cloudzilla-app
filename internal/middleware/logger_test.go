package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func serveLogged(pattern, path string) {
	r := chi.NewRouter()
	r.Use(Logger)
	r.Post(pattern, func(http.ResponseWriter, *http.Request) {})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
}

func TestLogger_TokenRoute_LogsPatternNotToken(t *testing.T) {
	logs := captureLogs(t)

	serveLogged("/register/complete/{token}", "/register/complete/secret123")

	if out := logs.String(); !strings.Contains(out, "/register/complete/{token}") || strings.Contains(out, "secret123") {
		t.Errorf("want the route pattern logged instead of the token:\n%s", out)
	}
}

func TestLogger_OtherRoute_LogsPath(t *testing.T) {
	logs := captureLogs(t)

	serveLogged("/{owner}/{repo}", "/alice/project")

	if out := logs.String(); !strings.Contains(out, "path=/alice/project") {
		t.Errorf("want the request path logged:\n%s", out)
	}
}
