package handler

import (
	"strings"
	"testing"
	"time"
)

func TestDeviceCookie(t *testing.T) {
	const secret = "s3cret"
	now := time.Now()
	valid := signDeviceCode(secret, 7, "BCDFGHJK", now.Add(time.Minute))
	payload, mac, _ := strings.Cut(valid, ".")

	flip := func(s string) string {
		b := []byte(s)
		if b[0] == 'A' {
			b[0] = 'B'
		} else {
			b[0] = 'A'
		}
		return string(b)
	}
	cases := []struct {
		name   string
		value  string
		secret string
		user   int64
		now    time.Time
		want   string
	}{
		{"round trip", valid, secret, 7, now, "BCDFGHJK"},
		{"tampered payload", flip(payload) + "." + mac, secret, 7, now, ""},
		{"tampered mac", payload + "." + flip(mac), secret, 7, now, ""},
		{"wrong user", valid, secret, 8, now, ""},
		{"wrong secret", valid, "other", 7, now, ""},
		{"expired", valid, secret, 7, now.Add(2 * time.Minute), ""},
		{"empty", "", secret, 7, now, ""},
		{"a raw user code", "BCDFGHJK", secret, 7, now, ""},
		{"garbage", "!!.??", secret, 7, now, ""},
		{"no mac", payload + ".", secret, 7, now, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := verifyDeviceCode(tc.secret, tc.value, tc.user, tc.now)
			if got != tc.want || ok != (tc.want != "") {
				t.Errorf("verify = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}
