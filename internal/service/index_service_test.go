package service

import "testing"

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"plain text", []byte("package main\nfunc main() {}\n"), false},
		{"null byte", []byte("hello\x00world"), true},
		{"empty", []byte{}, false},
		{"binary header", []byte{0x7f, 0x45, 0x4c, 0x46, 0x02}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinaryContent(tt.data); got != tt.want {
				t.Errorf("isBinaryContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
