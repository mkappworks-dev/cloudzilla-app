package service

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestEmailService_Enabled(t *testing.T) {
	if NewEmailService(config.SMTPConfig{}).Enabled() {
		t.Error("no SMTP host must mean disabled")
	}
	if !NewEmailService(config.SMTPConfig{Host: "smtp.test"}).Enabled() {
		t.Error("an SMTP host must mean enabled")
	}
}

func TestEmailService_Send_StalledServer_TimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})
	s := NewEmailService(config.SMTPConfig{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, From: "cz@test.invalid"})
	s.timeout = 100 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- s.Send("user@test.invalid", "subject", "body") }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("want an error from a server that never answers")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send is still waiting on a server that never answers")
	}
}

func TestSignupLinkEmail_LinksAndEscapes(t *testing.T) {
	_, body := signupLinkEmail("https://cz.test/register/complete/abc")
	if !strings.Contains(body, `href="https://cz.test/register/complete/abc"`) {
		t.Errorf("body must link to the completion page:\n%s", body)
	}
	_, body = signupLinkEmail(`https://cz.test/"><script>x</script>`)
	if strings.Contains(body, "<script>") {
		t.Errorf("link must be HTML-escaped:\n%s", body)
	}
}

func TestAccountExistsEmail_LinksToSignIn(t *testing.T) {
	_, body := accountExistsEmail("https://cz.test/login")
	if !strings.Contains(body, `href="https://cz.test/login"`) {
		t.Errorf("body must link to sign-in:\n%s", body)
	}
}
