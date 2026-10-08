package service

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func bindResponse(resultCode byte) []byte {
	return []byte{0x30, 0x0c, 0x02, 0x01, 0x01, 0x61, 0x07, 0x0a, 0x01, resultCode, 0x04, 0x00, 0x04, 0x00}
}

// fakeLDAP answers every bind with resultCode and reports the raw BindRequests
// it received.
func fakeLDAP(t *testing.T, resultCode byte) (host, port string, requests <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan []byte, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				req, err := readLDAPResponse(conn)
				if err != nil {
					return
				}
				got <- req
				_, _ = conn.Write(bindResponse(resultCode))
			}()
		}
	}()
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	return host, port, got
}

func TestEscapeLDAPDN(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"alice", "alice"},
		{"a,b", `a\,b`},
		{"a=b+c", `a\=b\+c`},
		{`a<b>c#d;e`, `a\<b\>c\#d\;e`},
		{`back\slash`, `back\\slash`},
		{`q"uote`, `q\"uote`},
		{" lead", `\ lead`},
		{"trail ", `trail\ `},
		{"in side", "in side"},
		{"nul\x00byte", `nul\00byte`},
		{"tab\there", `tab\09here`},
		{"del\x7f", `del\7F`},
		{"naïve", "naïve"},
		{"cn=admin,dc=x", `cn\=admin\,dc\=x`},
		{"", ""},
	} {
		if got := escapeLDAPDN(tc.in); got != tc.want {
			t.Errorf("escapeLDAPDN(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBerWrap_LengthForms(t *testing.T) {
	short := berWrap(0x04, make([]byte, 127))
	if !bytes.HasPrefix(short, []byte{0x04, 127}) || len(short) != 129 {
		t.Errorf("127-byte value: header % x, len %d", short[:2], len(short))
	}
	oneByte := berWrap(0x04, make([]byte, 200))
	if !bytes.HasPrefix(oneByte, []byte{0x04, 0x81, 200}) || len(oneByte) != 203 {
		t.Errorf("200-byte value: header % x, len %d", oneByte[:3], len(oneByte))
	}
	twoByte := berWrap(0x04, make([]byte, 300))
	if !bytes.HasPrefix(twoByte, []byte{0x04, 0x82, 0x01, 0x2c}) || len(twoByte) != 304 {
		t.Errorf("300-byte value: header % x, len %d", twoByte[:4], len(twoByte))
	}
}

func TestEncodeLDAPBindRequest(t *testing.T) {
	got := encodeLDAPBindRequest(1, "cn=a", "pw")
	want := []byte{
		0x30, 0x12, // LDAPMessage
		0x02, 0x01, 0x01, // messageID 1
		0x60, 0x0d, // BindRequest
		0x02, 0x01, 0x03, // version 3
		0x04, 0x04, 'c', 'n', '=', 'a', // name
		0x80, 0x02, 'p', 'w', // simple auth
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got % x\nwant % x", got, want)
	}
}

func TestParseLDAPBindResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"success", bindResponse(0), ""},
		{"invalid credentials", bindResponse(49), "resultCode=49"},
		{"empty", nil, "too short"},
		{"one byte", []byte{0x30}, "too short"},
		{"wrong protocol op", []byte{0x30, 0x05, 0x02, 0x01, 0x01, 0x64, 0x00}, "unexpected protocol op"},
		{"no result code", []byte{0x30, 0x07, 0x02, 0x01, 0x01, 0x61, 0x02, 0x04, 0x00}, "missing resultCode"},
	} {
		err := parseLDAPBindResponse(tc.data)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: got %v, want error containing %q", tc.name, err, tc.wantErr)
		}
	}
}

// A hostile directory controls these bytes; truncation must be an error, not a panic.
func TestParseLDAPBindResponse_TruncatedInputNeverPanics(t *testing.T) {
	full := bindResponse(0)
	for n := 0; n < len(full); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %d-byte prefix: %v", n, r)
				}
			}()
			_ = parseLDAPBindResponse(full[:n])
		}()
	}
}

func TestReadLDAPResponse(t *testing.T) {
	long := append([]byte{0x30, 0x81, 200}, make([]byte, 200)...)
	for _, tc := range []struct {
		name    string
		in      []byte
		want    []byte
		wantErr bool
	}{
		{"short form", bindResponse(0), bindResponse(0), false},
		{"long form keeps its length bytes", long, long, false},
		{"length wider than four bytes", []byte{0x30, 0x85, 1, 2, 3, 4, 5}, nil, true},
		{"body shorter than declared", []byte{0x30, 0x05, 0x01}, nil, true},
	} {
		client, server := net.Pipe()
		go func() {
			_, _ = server.Write(tc.in)
			_ = server.Close()
		}()
		got, err := readLDAPResponse(client)
		_ = client.Close()
		if (err != nil) != tc.wantErr || !bytes.Equal(got, tc.want) {
			t.Errorf("%s: got % x, %v", tc.name, got, err)
		}
	}
}

func TestBindLDAP(t *testing.T) {
	host, port, requests := fakeLDAP(t, 0)
	if err := bindLDAP(net.JoinHostPort(host, port), "cn=a", "pw", false); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; !bytes.Equal(got, encodeLDAPBindRequest(1, "cn=a", "pw")) {
		t.Errorf("server received % x", got)
	}

	host, port, _ = fakeLDAP(t, 49)
	err := bindLDAP(net.JoinHostPort(host, port), "cn=a", "wrong", false)
	if err == nil || !strings.Contains(err.Error(), "resultCode=49") {
		t.Errorf("wrong password: got %v", err)
	}
}

func TestBindLDAP_UnreachableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	if err := bindLDAP(addr, "cn=a", "pw", false); err == nil || !strings.Contains(err.Error(), "ldap dial") {
		t.Errorf("got %v, want a dial error", err)
	}
}

func saveLDAP(t *testing.T, svc *SSOService, host, port string, enabled bool) {
	t.Helper()
	err := svc.SetConfig(context.Background(), "ldap", map[string]string{
		model.LDAPKeyHost: host, model.LDAPKeyPort: port, model.LDAPKeyBindDNTmpl: "uid=%s,ou=people,dc=test",
	}, enabled)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticateLDAP_ProvisionsThenRecognisesTheUser(t *testing.T) {
	svc, _ := newSSOTestService(t)
	host, port, requests := fakeLDAP(t, 0)
	saveLDAP(t, svc, host, port, true)
	ctx := context.Background()

	u, token, err := svc.AuthenticateLDAP(ctx, "ann", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "ann" || token == "" {
		t.Errorf("user %q, token issued = %v", u.Username, token != "")
	}
	want := encodeLDAPBindRequest(1, "uid=ann,ou=people,dc=test", "pw")
	if got := <-requests; !bytes.Equal(got, want) {
		t.Errorf("bind request % x, want % x", got, want)
	}

	again, _, err := svc.AuthenticateLDAP(ctx, "ann", "pw")
	if err != nil || again.ID != u.ID {
		t.Errorf("second sign-in: user %v, err %v; want user %d", again, err, u.ID)
	}
}

// The username lands in a DN template; an attacker must not be able to
// change which entry is bound.
func TestAuthenticateLDAP_EscapesTheUsernameInTheBindDN(t *testing.T) {
	svc, _ := newSSOTestService(t)
	host, port, requests := fakeLDAP(t, 0)
	saveLDAP(t, svc, host, port, true)

	if _, _, err := svc.AuthenticateLDAP(context.Background(), "x,ou=admins", "pw"); err != nil {
		t.Fatal(err)
	}
	want := encodeLDAPBindRequest(1, `uid=x\,ou\=admins,ou=people,dc=test`, "pw")
	if got := <-requests; !bytes.Equal(got, want) {
		t.Errorf("bind request % x, want % x", got, want)
	}
}

func TestAuthenticateLDAP_WrongPasswordProvisionsNothing(t *testing.T) {
	svc, db := newSSOTestService(t)
	host, port, _ := fakeLDAP(t, 49)
	saveLDAP(t, svc, host, port, true)

	u, token, err := svc.AuthenticateLDAP(context.Background(), "mallory", "bad")
	if err == nil || u != nil || token != "" {
		t.Fatalf("got %v, %q, %v; want a refusal", u, token, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE sso_provider = 'ldap'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("failed bind left %d ldap users (err %v)", n, err)
	}
}

func TestAuthenticateLDAP_Preconditions(t *testing.T) {
	ctx := context.Background()

	t.Run("not configured", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		if _, _, err := svc.AuthenticateLDAP(ctx, "a", "p"); err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		saveLDAP(t, svc, "127.0.0.1", "1", false)
		if _, _, err := svc.AuthenticateLDAP(ctx, "a", "p"); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("incomplete", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		if err := svc.SetConfig(ctx, "ldap", map[string]string{model.LDAPKeyHost: "h"}, true); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.AuthenticateLDAP(ctx, "a", "p"); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("login switched off site-wide", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		host, port, requests := fakeLDAP(t, 0)
		saveLDAP(t, svc, host, port, true)
		if err := svc.siteSetting.Set(ctx, "allow_login", "false"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.AuthenticateLDAP(ctx, "a", "p"); !errors.Is(err, ErrLoginDisabled) {
			t.Errorf("got %v, want ErrLoginDisabled", err)
		}
		select {
		case <-requests:
			t.Error("directory was contacted while login is disabled")
		default:
		}
	})
	t.Run("registration closed", func(t *testing.T) {
		svc, _ := newSSOTestService(t)
		host, port, _ := fakeLDAP(t, 0)
		saveLDAP(t, svc, host, port, true)
		if err := svc.siteSetting.Set(ctx, "allow_registration", "false"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.AuthenticateLDAP(ctx, "newbie", "p"); !errors.Is(err, ErrRegistrationDisabled) {
			t.Errorf("got %v, want ErrRegistrationDisabled", err)
		}
	})
}

func TestAuthenticateLDAP_DefaultPortIs389(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	// Nothing listens on 389 in the test environment, so the dial error names the port used.
	if err := svc.SetConfig(ctx, "ldap", map[string]string{
		model.LDAPKeyHost: "127.0.0.1", model.LDAPKeyBindDNTmpl: "uid=%s",
	}, true); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.AuthenticateLDAP(ctx, "a", "p")
	if err == nil {
		t.Skip("something is listening on 127.0.0.1:389")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:"+strconv.Itoa(389)) {
		t.Errorf("got %v, want a dial to port 389", err)
	}
}

func TestCheckLDAPPassword(t *testing.T) {
	ctx := context.Background()

	svc, _ := newSSOTestService(t)
	if err := svc.CheckLDAPPassword(ctx, "", "pw"); err == nil {
		t.Error("empty bind DN accepted")
	}
	if err := svc.CheckLDAPPassword(ctx, "cn=a", ""); err == nil {
		t.Error("empty password accepted: an anonymous bind would succeed against most directories")
	}
	if err := svc.CheckLDAPPassword(ctx, "cn=a", "pw"); err == nil {
		t.Error("check passed with no ldap config")
	}

	host, port, requests := fakeLDAP(t, 0)
	saveLDAP(t, svc, host, port, false)
	if err := svc.CheckLDAPPassword(ctx, "cn=a", "pw"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled provider: got %v", err)
	}
	saveLDAP(t, svc, host, port, true)
	if err := svc.CheckLDAPPassword(ctx, "cn=a", "pw"); err != nil {
		t.Fatalf("correct password: %v", err)
	}
	if got := <-requests; !bytes.Equal(got, encodeLDAPBindRequest(1, "cn=a", "pw")) {
		t.Errorf("bind DN is used verbatim: server received % x", got)
	}

	host, port, _ = fakeLDAP(t, 49)
	saveLDAP(t, svc, host, port, true)
	if err := svc.CheckLDAPPassword(ctx, "cn=a", "wrong"); err == nil {
		t.Error("wrong password accepted")
	}
}
