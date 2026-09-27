package service

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestNoreplyHostFromBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://git.example.com":       "git.example.com",
		"http://localhost:8080":         "localhost",
		"https://git.example.com:8443/": "git.example.com",
		"":                              "localhost",
		"::not a url":                   "localhost",
	}
	for in, want := range cases {
		if got := noreplyHostFromBaseURL(in); got != want {
			t.Errorf("noreplyHostFromBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoreplyEmail_RoundTrips(t *testing.T) {
	addr := noreplyEmail("git.example.com", &model.User{ID: 42, Username: "alice"})
	if addr != "42+alice@users.noreply.git.example.com" {
		t.Fatalf("noreplyEmail = %q", addr)
	}
	id, username, ok := parseNoreplyEmail(addr)
	if !ok || id != 42 || username != "alice" {
		t.Fatalf("parseNoreplyEmail(%q) = %d, %q, %v", addr, id, username, ok)
	}
}

func TestParseNoreplyEmail(t *testing.T) {
	cases := []struct {
		email    string
		id       int64
		username string
		ok       bool
	}{
		{"7+bob@users.noreply.localhost", 7, "bob", true},
		{"7+bob@USERS.NOREPLY.example.com", 7, "bob", true},
		{"7+bob@users.noreply.", 0, "", false},
		{"bob@users.noreply.localhost", 0, "", false},
		{"x+bob@users.noreply.localhost", 0, "", false},
		{"0+bob@users.noreply.localhost", 0, "", false},
		{"-3+bob@users.noreply.localhost", 0, "", false},
		{"7+@users.noreply.localhost", 0, "", false},
		{"7+bob@example.com", 0, "", false},
		{"bob@localhost", 0, "", false},
		{"not-an-email", 0, "", false},
	}
	for _, c := range cases {
		id, username, ok := parseNoreplyEmail(c.email)
		if id != c.id || username != c.username || ok != c.ok {
			t.Errorf("parseNoreplyEmail(%q) = %d, %q, %v; want %d, %q, %v",
				c.email, id, username, ok, c.id, c.username, c.ok)
		}
	}
}
