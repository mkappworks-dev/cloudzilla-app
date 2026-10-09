package testutil

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// FakeLDAP listens for simple binds and accepts one whose request carries password
// in the clear, as an LDAP simple bind does; any other bind gets invalidCredentials.
func FakeLDAP(t *testing.T, password string) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake ldap listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 4096)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				result := byte(49)
				if bytes.Contains(buf[:n], []byte(password)) {
					result = 0
				}
				_, _ = conn.Write([]byte{0x30, 0x0c, 0x02, 0x01, 0x01, 0x61, 0x07, 0x0a, 0x01, result, 0x04, 0x00, 0x04, 0x00})
			}()
		}
	}()
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	return host, port
}
