package handler

import (
	"testing"

	"golang.org/x/oauth2"
)

// UseFakeGoogle points the Google OAuth token and userinfo calls at baseURL until t ends.
func UseFakeGoogle(t testing.TB, baseURL string) {
	endpoint, userinfo := googleEndpoint, googleUserinfoURL
	googleEndpoint = oauth2.Endpoint{AuthURL: baseURL + "/auth", TokenURL: baseURL + "/token"}
	googleUserinfoURL = baseURL + "/userinfo"
	t.Cleanup(func() { googleEndpoint, googleUserinfoURL = endpoint, userinfo })
}

var SafeNextPath = safeNextPath

const MaxRawBlobBytes = maxRawBlobBytes
