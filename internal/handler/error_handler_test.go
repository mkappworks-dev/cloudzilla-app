package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverer_PanicBecomesJSON500ForAPI(t *testing.T) {
	h := &Handler{}
	rr := httptest.NewRecorder()
	h.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })).
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

func TestRecoverer_LetsAbortHandlerThrough(t *testing.T) {
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", rec)
		}
	}()
	h := &Handler{}
	h.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}
