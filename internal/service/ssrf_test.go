package service

import "testing"

func TestIsInternalURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantTrue bool // true = blocked (internal)
	}{
		// Should block
		{name: "localhost", url: "http://localhost/hook", wantTrue: true},
		{name: "localhost with port", url: "http://localhost:8080/hook", wantTrue: true},
		{name: "loopback IPv4", url: "http://127.0.0.1/hook", wantTrue: true},
		{name: "loopback IPv4 with port", url: "http://127.0.0.1:9090/hook", wantTrue: true},
		{name: "private 10.x", url: "http://10.0.0.1/hook", wantTrue: true},
		{name: "private 172.16.x", url: "http://172.16.0.1/hook", wantTrue: true},
		{name: "private 192.168.x", url: "http://192.168.1.1/hook", wantTrue: true},
		{name: "link-local", url: "http://169.254.169.254/latest/meta-data/", wantTrue: true},
		{name: "google metadata", url: "http://metadata.google.internal/", wantTrue: true},
		{name: "IPv6 loopback", url: "http://[::1]/hook", wantTrue: true},
		{name: "unparseable URL", url: "://bad", wantTrue: true},
		{name: "empty URL", url: "", wantTrue: true},

		// Should allow
		{name: "public domain", url: "http://example.com/hook", wantTrue: false},
		{name: "public HTTPS", url: "https://hooks.example.com/webhook", wantTrue: false},
		{name: "public IP", url: "http://8.8.8.8/hook", wantTrue: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInternalURL(tt.url)
			if got != tt.wantTrue {
				t.Errorf("isInternalURL(%q) = %v, want %v", tt.url, got, tt.wantTrue)
			}
		})
	}
}
