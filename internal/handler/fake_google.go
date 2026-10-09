package handler

import "golang.org/x/oauth2"

// UseFakeGoogle points the Google OAuth token and userinfo calls at baseURL until t
// ends. It lives outside _test.go so router tests can use it, and takes a Cleanup
// interface rather than testing.TB to keep "testing" out of the server binary.
func UseFakeGoogle(t interface{ Cleanup(func()) }, baseURL string) {
	endpoint, userinfo := googleEndpoint, googleUserinfoURL
	googleEndpoint = oauth2.Endpoint{AuthURL: baseURL + "/auth", TokenURL: baseURL + "/token"}
	googleUserinfoURL = baseURL + "/userinfo"
	t.Cleanup(func() { googleEndpoint, googleUserinfoURL = endpoint, userinfo })
}
