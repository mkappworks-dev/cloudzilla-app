package logmask

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestMask_HidesTheValueAndItsEncodedForms(t *testing.T) {
	secret := "p@ss w0rd/+="
	m := New([]string{secret})
	for name, in := range map[string]string{
		"exact":      "token is " + secret + " ok",
		"base64":     "auth " + base64.StdEncoding.EncodeToString([]byte(secret)),
		"base64 url": "auth " + base64.URLEncoding.EncodeToString([]byte(secret)),
		"query":      "?k=" + url.QueryEscape(secret),
		"path":       "/x/" + url.PathEscape(secret),
		"twice":      secret + secret,
	} {
		out := m.Mask(in)
		if strings.Contains(out, "p@ss") || strings.Contains(out, "p%40ss") || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(secret))) {
			t.Errorf("%s: %q leaked in %q", name, secret, out)
		}
		if !strings.Contains(out, Redacted) {
			t.Errorf("%s: no redaction in %q", name, out)
		}
	}
}

func TestMask_NothingToHideChangesNothing(t *testing.T) {
	for _, m := range []*Masker{nil, New(nil), New([]string{""})} {
		if got := m.Mask("plain text"); got != "plain text" {
			t.Errorf("got %q", got)
		}
	}
}

func TestMask_LongerSecretWinsOverItsPrefix(t *testing.T) {
	m := New([]string{"abcd", "abcdefgh"})
	if got := m.Mask("x abcdefgh y"); got != "x "+Redacted+" y" {
		t.Errorf("got %q", got)
	}
}

func TestCombineMasksBothSets(t *testing.T) {
	m := Combine(New([]string{"runner-token"}), nil, New([]string{"job-secret"}))
	if got := m.Mask("a runner-token b job-secret"); got != "a *** b ***" {
		t.Errorf("got %q", got)
	}
}
