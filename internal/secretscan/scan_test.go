package secretscan

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const maxBlob = 1 << 20

var czpToken = "czp_" + strings.Repeat("ab12", 16)

func TestScan_Patterns(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantType string // "" means no finding
		wantLine int
		wantConf Confidence
	}{
		{"aws hit", "id = AKIAIOSFODNN7EXAMPLE\n", "aws_access_key_id", 1, High},
		{"aws asia hit", "x\nASIAIOSFODNN7EXAMPLE", "aws_access_key_id", 2, High},
		{"aws near miss, AKIA plus 15", "AKIAIOSFODNN7EXAMPL", "", 0, 0},
		{"aws false positive, lowercase", "akiaiosfodnn7example", "", 0, 0},
		{"aws false positive, embedded in word", "XAKIAIOSFODNN7EXAMPLE", "", 0, 0},

		{"rsa key hit", "a\nb\n-----BEGIN RSA PRIVATE KEY-----\nMIIE\n", "private_key", 3, High},
		{"openssh key hit", "-----BEGIN OPENSSH PRIVATE KEY-----", "private_key", 1, High},
		{"pgp key hit", "-----BEGIN PGP PRIVATE KEY BLOCK-----", "private_key", 1, High},
		{"plain key hit", "-----BEGIN PRIVATE KEY-----", "private_key", 1, High},
		{"public key near miss", "-----BEGIN PUBLIC KEY-----", "", 0, 0},
		{"prose false positive", "a private key is secret", "", 0, 0},

		{"token hit", "Authorization: " + czpToken, "cloudzilla_token", 1, High},
		{"token near miss, 63 hex", "czp_" + strings.Repeat("a", 63), "", 0, 0},
		{"token false positive, non-hex", "czp_" + strings.Repeat("z", 64), "", 0, 0},

		{"jwt hit", `JWT_SECRET = "s3cr3t-value-0123456789"`, "jwt_secret", 1, High},
		{"jwt lowercase hit", "jwt_secret: 'abcdefghijklmnop'", "jwt_secret", 1, High},
		{"jwt near miss, short", `JWT_SECRET="short"`, "", 0, 0},
		{"jwt false positive, env reference", `JWT_SECRET=${JWT_SECRET_FROM_VAULT_LONG}`, "", 0, 0},

		{"generic hit", `api_key = "9fT3kQ8zLm2XvB7nRw4YpC1dHs6J"`, "generic_api_key", 1, Low},
		{"generic hit, token", `token: "Zx9Qm4Lk2Vb8Nc7Rt5Yp3Hd1Jw6Sf0A"`, "generic_api_key", 1, Low},
		{"generic near miss, short", `api_key = "abc123"`, "", 0, 0},
		{"generic false positive, low entropy", `api_key = "aaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, "", 0, 0},
		{"generic false positive, placeholder", `api_key = "your-api-key-goes-here-please"`, "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Scan("f.txt", []byte(tt.content), maxBlob)
			if tt.wantType == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %d (%s)", len(got), got[0])
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want 1 finding, got %d", len(got))
			}
			f := got[0]
			if f.Type != tt.wantType || f.Line != tt.wantLine || f.Confidence != tt.wantConf {
				t.Errorf("got type=%s line=%d conf=%v", f.Type, f.Line, f.Confidence)
			}
		})
	}
}

func TestScan_Values(t *testing.T) {
	got := Scan("f", []byte("AKIAIOSFODNN7EXAMPLE"), maxBlob)
	if len(got) != 1 || got[0].Value != "AKIAIOSFODNN7EXAMPLE" {
		t.Fatalf("aws value: %+v", got)
	}
	got = Scan("f", []byte("x\n-----BEGIN RSA PRIVATE KEY-----\nabc"), maxBlob)
	if len(got) != 1 || got[0].Value != "-----BEGIN RSA PRIVATE KEY-----" {
		t.Fatalf("key value should be the header line: %+v", got)
	}
	got = Scan("f", []byte(`JWT_SECRET = "s3cr3t-value-0123456789"`), maxBlob)
	if len(got) != 1 || got[0].Value != "s3cr3t-value-0123456789" {
		t.Fatalf("jwt value should be the literal: %+v", got)
	}
}

func TestScan_Skips(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"

	t.Run("binary", func(t *testing.T) {
		c := append([]byte("\x00"), key...)
		if got := Scan("f.bin", c, maxBlob); len(got) != 0 {
			t.Fatalf("binary blob scanned: %d", len(got))
		}
	})
	t.Run("nul after 8 KiB is text", func(t *testing.T) {
		c := bytes.Repeat([]byte("a\n"), 4097)
		c = append(c, key+"\n\x00"...)
		if got := Scan("f", c, maxBlob); len(got) != 1 {
			t.Fatalf("want 1, got %d", len(got))
		}
	})
	t.Run("over size", func(t *testing.T) {
		c := []byte(key + strings.Repeat(" ", 100))
		if got := Scan("f", c, 50); len(got) != 0 {
			t.Fatalf("over-size blob scanned: %d", len(got))
		}
		if got := Scan("f", c, len(c)); len(got) != 1 {
			t.Fatalf("blob at the limit must be scanned: %d", len(got))
		}
	})
	t.Run("cap at 50", func(t *testing.T) {
		var b strings.Builder
		for i := range 200 {
			fmt.Fprintf(&b, "AKIAIOSFODNN7EXAAA%c%c\n", 'A'+i/26, 'A'+i%26)
		}
		got := Scan("f", []byte(b.String()), maxBlob)
		if len(got) != 50 {
			t.Fatalf("want 50, got %d", len(got))
		}
	})
}

func TestMask(t *testing.T) {
	tests := map[string]string{
		"AKIAIOSFODNN7EXAMPLE":                "AKIAMPLE",
		"-----BEGIN RSA PRIVATE KEY-----":     "RSA",
		"-----BEGIN PRIVATE KEY-----":         "PRIVATE",
		"-----BEGIN OPENSSH PRIVATE KEY-----": "OPENSSH",
		"short":                               "****",
		"":                                    "****",
	}
	for in, want := range tests {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{czpToken, strings.Repeat("é", 30), "-----BEGIN ENCRYPTED PRIVATE KEY-----"} {
		if n := len([]rune(Mask(in))); n > 8 {
			t.Errorf("Mask(%q) returned %d chars", in, n)
		}
	}
}

func TestHash(t *testing.T) {
	a := Hash(1, "v")
	if a != Hash(1, "v") {
		t.Error("hash not stable")
	}
	if a == Hash(2, "v") {
		t.Error("hash equal across repos")
	}
	if a == Hash(1, "w") {
		t.Error("hash equal across values")
	}
	const want = "0c5de15765b4dd2dfcf354493709ffcadc55c8aa28ed7f738bf1d1e6fd921b39"
	if a != want {
		t.Errorf("Hash(1, v) = %s, want sha256 of \"1:v\"", a)
	}
}

func TestFinding_NeverLogsValue(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	found := Scan("f", []byte("k = "+key), maxBlob)
	if len(found) != 1 {
		t.Fatalf("want 1 finding, got %d", len(found))
	}
	f := found[0]
	log.Info("found", "finding", f)
	log.Info("found", "finding", &f)
	fmt.Fprintf(&buf, "%v %+v %s %#v", f, f, f, f)
	out := buf.String()
	if !strings.Contains(out, "aws_access_key_id") {
		t.Fatalf("expected type in output: %q", out)
	}
	if strings.Contains(out, key) || strings.Contains(out, "IOSFODNN") {
		t.Fatalf("value leaked: %q", out)
	}
}
