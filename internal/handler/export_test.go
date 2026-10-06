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

// UseExpandAllBudget caps how many folders Expand all opens until t ends.
func UseExpandAllBudget(t testing.TB, n int) {
	prev := expandAllBudget
	expandAllBudget = n
	t.Cleanup(func() { expandAllBudget = prev })
}

var SafeNextPath = safeNextPath

const MaxRawBlobBytes = maxRawBlobBytes
