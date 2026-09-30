package testutil

import (
	"bufio"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// Mail is one message the fake SMTP server accepted.
type Mail struct {
	To   []string
	Data string
}

// Mailbox receives what a FakeSMTP server accepts.
type Mailbox struct {
	ch chan Mail
	// held keeps messages NextTo passed over, in arrival order, for Next and Empty.
	held []Mail
}

// Next waits for the next message, failing t if none arrives in time.
func (m *Mailbox) Next(t *testing.T) Mail {
	t.Helper()
	if len(m.held) > 0 {
		mail := m.held[0]
		m.held = m.held[1:]
		return mail
	}
	select {
	case mail := <-m.ch:
		return mail
	case <-time.After(5 * time.Second):
		t.Fatal("no email arrived")
		return Mail{}
	}
}

// NextTo waits for a message to addr. Messages to others stay in the mailbox,
// since mail sent from background goroutines arrives in no fixed order.
func (m *Mailbox) NextTo(t *testing.T, addr string) Mail {
	t.Helper()
	for i, mail := range m.held {
		if len(mail.To) == 1 && mail.To[0] == addr {
			m.held = append(m.held[:i], m.held[i+1:]...)
			return mail
		}
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case mail := <-m.ch:
			if len(mail.To) == 1 && mail.To[0] == addr {
				return mail
			}
			m.held = append(m.held, mail)
		case <-deadline:
			t.Fatalf("no email to %s arrived", addr)
			return Mail{}
		}
	}
}

// Drain returns every message waiting or arriving within wait.
func (m *Mailbox) Drain(wait time.Duration) []Mail {
	got := m.held
	m.held = nil
	deadline := time.After(wait)
	for {
		select {
		case mail := <-m.ch:
			got = append(got, mail)
		case <-deadline:
			return got
		}
	}
}

// Empty fails t if a message is waiting or arrives within wait.
func (m *Mailbox) Empty(t *testing.T, wait time.Duration) {
	t.Helper()
	if len(m.held) > 0 {
		t.Fatalf("unexpected email to %v", m.held[0].To)
	}
	select {
	case mail := <-m.ch:
		t.Fatalf("unexpected email to %v", mail.To)
	case <-time.After(wait):
	}
}

var verifyLinkRe = regexp.MustCompile(`https?://[^\s"<]+/verify-email\?token=([A-Za-z0-9_-]+)`)

// VerificationToken returns the token in the message's verification link.
func (m Mail) VerificationToken(t *testing.T) string {
	t.Helper()
	match := verifyLinkRe.FindStringSubmatch(m.Data)
	if match == nil {
		t.Fatalf("no verification link in email: %.500s", m.Data)
	}
	return match[1]
}

// FakeSMTP starts an SMTP server on 127.0.0.1 that accepts every message, and
// returns the config that sends to it. net/smtp sends PLAIN credentials
// without TLS only to localhost, which this satisfies.
func FakeSMTP(t *testing.T) (config.SMTPConfig, *Mailbox) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake smtp listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	box := &Mailbox{ch: make(chan Mail, 16)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSMTP(conn, box)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return config.SMTPConfig{Host: "127.0.0.1", Port: port, Username: "u", Password: "p", From: "cloudzilla@test.invalid"}, box
}

func serveSMTP(conn net.Conn, box *Mailbox) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 fake ESMTP")
	var mail Mail
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250-fake")
			reply("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			reply("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			mail = Mail{}
			reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			mail.To = append(mail.To, strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>"))
			reply("250 ok")
		case cmd == "DATA":
			reply("354 go ahead")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(l, "."))
			}
			mail.Data = data.String()
			box.ch <- mail
			reply("250 ok")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}
