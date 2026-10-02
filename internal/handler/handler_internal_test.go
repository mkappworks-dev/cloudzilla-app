package handler

import (
	"net/http/httptest"
	"testing"
)

func TestIsFormEncoded(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		want        bool
	}{
		{"application/x-www-form-urlencoded", true},
		{"application/x-www-form-urlencoded;charset=UTF-8", true},
		{"Application/X-WWW-Form-Urlencoded; charset=utf-8", true},
		{"multipart/form-data; boundary=x", false},
		{"application/json", false},
		{"", false},
	} {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("Content-Type", tc.contentType)
		if got := isFormEncoded(req); got != tc.want {
			t.Errorf("isFormEncoded(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}
