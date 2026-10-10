package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// --------------------------------------------------------------------------
// LDAP authentication
// --------------------------------------------------------------------------

// AuthenticateLDAP authenticates a user via LDAP simple bind.
// It dials the configured LDAP server, sends an LDAPv3 BindRequest, and checks the response.
// On success it looks up or provisions the local user account and returns a JWT.
func (s *SSOService) AuthenticateLDAP(ctx context.Context, username, password string) (*model.User, string, error) {
	if !s.siteSetting.AllowLogin(ctx) {
		return nil, "", ErrLoginDisabled
	}

	cfg, err := s.store.GetByProvider(ctx, "ldap")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("ldap sso not configured")
		}
		return nil, "", fmt.Errorf("ldap sso config lookup: %w", err)
	}
	if !cfg.Enabled {
		return nil, "", fmt.Errorf("ldap sso is disabled")
	}

	host := cfg.Config[model.LDAPKeyHost]
	port := cfg.Config[model.LDAPKeyPort]
	if port == "" {
		port = "389"
	}
	bindDNTmpl := cfg.Config[model.LDAPKeyBindDNTmpl]
	if host == "" || bindDNTmpl == "" {
		return nil, "", fmt.Errorf("ldap config incomplete: host and bind_dn_tmpl required")
	}

	// Escape the username per RFC 4514 before interpolating into the DN template
	// to prevent LDAP injection via DN special characters.
	bindDN := fmt.Sprintf(bindDNTmpl, escapeLDAPDN(username))
	addr := net.JoinHostPort(host, port)

	useTLS := cfg.Config[model.LDAPKeyUseTLS] == "true"
	if err := bindLDAP(addr, bindDN, password, useTLS); err != nil {
		return nil, "", fmt.Errorf("ldap authentication failed: %w", err)
	}

	// Derive a stable SSO ID (use the bind DN as the canonical unique identifier)
	ssoID := bindDN

	allowReg := s.siteSetting.AllowRegistration(ctx)
	return s.findOrProvisionUser(ctx, "ldap", ssoID, username, username+"@ldap.local", allowReg)
}

// CheckLDAPPassword binds as bindDN, the identity an LDAP account signs in with,
// to check its directory password.
func (s *SSOService) CheckLDAPPassword(ctx context.Context, bindDN, password string) error {
	if bindDN == "" || password == "" {
		return fmt.Errorf("ldap password check: missing bind DN or password")
	}
	cfg, err := s.store.GetByProvider(ctx, "ldap")
	if err != nil {
		return fmt.Errorf("ldap sso config lookup: %w", err)
	}
	if !cfg.Enabled {
		return fmt.Errorf("ldap sso is disabled")
	}
	port := cfg.Config[model.LDAPKeyPort]
	if port == "" {
		port = "389"
	}
	return bindLDAP(net.JoinHostPort(cfg.Config[model.LDAPKeyHost], port), bindDN, password, cfg.Config[model.LDAPKeyUseTLS] == "true")
}

// escapeLDAPDN escapes a string for safe interpolation into an LDAP DN per RFC 4514.
// The following characters are escaped with a leading backslash: , = + < > # ; \ "
// Control characters and non-ASCII bytes are hex-escaped as \XX.
// Leading and trailing spaces are also escaped.
func escapeLDAPDN(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, c := range runes {
		switch {
		case c == 0:
			b.WriteString("\\00")
		case c == '\\':
			b.WriteString("\\\\")
		case c == '"':
			b.WriteString("\\\"")
		case c == ',' || c == '=' || c == '+' || c == '<' || c == '>' || c == '#' || c == ';':
			b.WriteByte('\\')
			b.WriteRune(c)
		case c == ' ' && (i == 0 || i == len(runes)-1):
			b.WriteString("\\ ")
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%02X", c)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// ldapRootCAs verifies LDAPS server certificates; nil means the system roots.
// Only tests set it.
var ldapRootCAs *x509.CertPool

// bindLDAP performs an LDAPv3 simple bind.
// BER encoding for BindRequest (Application tag 0):
//
//	BindRequest ::= [APPLICATION 0] SEQUENCE {
//	    version   INTEGER (1..127),
//	    name      LDAPDN,
//	    authentication AuthenticationChoice }
//	AuthenticationChoice ::= CHOICE {
//	    simple  [0] OCTET STRING }
func bindLDAP(addr, dn, password string, useTLS bool) error {
	var conn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if useTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: ldapRootCAs})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("ldap dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := encodeLDAPBindRequest(1, dn, password)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("ldap write: %w", err)
	}

	resp, err := readLDAPResponse(conn)
	if err != nil {
		return fmt.Errorf("ldap read: %w", err)
	}

	return parseLDAPBindResponse(resp)
}

// encodeLDAPBindRequest encodes an LDAPv3 BindRequest as a BER-encoded byte slice.
// The message ID is fixed at 1.
//
// LDAPMessage structure:
//
//	SEQUENCE {
//	    messageID  INTEGER,
//	    protocolOp BindRequest }
func encodeLDAPBindRequest(msgID int, dn, password string) []byte {
	// Encode BindRequest body: version(3), name(dn), authentication(simple=password)
	version := berEncodeInteger(3)
	name := berEncodeOctetString(dn)
	simpleAuth := berEncodeContextPrimitive(0, []byte(password)) // [0] OCTET STRING

	bindBody := append(version, name...)
	bindBody = append(bindBody, simpleAuth...)

	// Wrap in APPLICATION 0 (BindRequest)
	bindReq := berWrap(0x60, bindBody) // 0x60 = APPLICATION CONSTRUCTED 0

	// Wrap in LDAPMessage SEQUENCE
	msgIDBytes := berEncodeInteger(msgID)
	msgBody := append(msgIDBytes, bindReq...)
	return berWrap(0x30, msgBody) // 0x30 = UNIVERSAL CONSTRUCTED SEQUENCE
}

// berEncodeInteger encodes a small non-negative integer as BER INTEGER TLV.
func berEncodeInteger(n int) []byte {
	return berWrap(0x02, []byte{byte(n)})
}

// berEncodeOctetString encodes a UTF-8 string as BER OCTET STRING TLV.
func berEncodeOctetString(s string) []byte {
	return berWrap(0x04, []byte(s))
}

// berEncodeContextPrimitive encodes a context-specific primitive TLV (e.g. [0]).
func berEncodeContextPrimitive(tag byte, value []byte) []byte {
	return berWrap(0x80|tag, value) // 0x80 = context-specific primitive
}

// berWrap wraps a value with a BER tag and length.
func berWrap(tag byte, value []byte) []byte {
	l := len(value)
	var lenBytes []byte
	if l < 128 {
		lenBytes = []byte{byte(l)}
	} else if l < 256 {
		lenBytes = []byte{0x81, byte(l)}
	} else {
		lenBytes = []byte{0x82, byte(l >> 8), byte(l)}
	}
	result := []byte{tag}
	result = append(result, lenBytes...)
	result = append(result, value...)
	return result
}

// readLDAPResponse reads exactly one BER TLV from conn.
func readLDAPResponse(conn net.Conn) ([]byte, error) {
	// Read tag + first length byte.
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := int(header[1])
	// lenBuf holds the additional length bytes for long-form BER encoding.
	var lenBuf []byte
	if length&0x80 != 0 {
		numBytes := length & 0x7f
		if numBytes > 4 {
			return nil, fmt.Errorf("ldap response length too large")
		}
		lenBuf = make([]byte, numBytes)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("read length bytes: %w", err)
		}
		length = 0
		for _, b := range lenBuf {
			length = length<<8 | int(b)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	// Reconstruct the full TLV: tag byte + length indicator byte + (optional
	// multi-byte length value) + body. Previously lenBuf was left out of the
	// result, zeroing those bytes and corrupting the parser for responses >127 B.
	result := make([]byte, 0, 2+len(lenBuf)+length)
	result = append(result, header...)
	result = append(result, lenBuf...)
	result = append(result, body...)
	return result, nil
}

// parseLDAPBindResponse extracts the resultCode from an LDAPMessage BindResponse.
// A resultCode of 0 means success.
func parseLDAPBindResponse(data []byte) error {
	// data is: SEQUENCE { INTEGER(msgID), [APPLICATION 1] SEQUENCE { ENUM(resultCode), ... } }
	// We need to navigate: outer SEQUENCE body → skip msgID → APPLICATION 1 body → first ENUM byte
	if len(data) < 2 {
		return fmt.Errorf("ldap response too short")
	}
	// Skip outer SEQUENCE tag + length
	pos := 2
	if data[1]&0x80 != 0 {
		pos += int(data[1] & 0x7f)
	}
	if pos >= len(data) {
		return fmt.Errorf("ldap response truncated after outer sequence")
	}
	// Skip messageID: tag(0x02) + length + value
	if pos+2 > len(data) {
		return fmt.Errorf("ldap response truncated at messageID")
	}
	pos += 2 + int(data[pos+1]) // skip INTEGER TLV
	// Now at APPLICATION 1 (BindResponse = tag 0x61)
	if pos+2 > len(data) {
		return fmt.Errorf("ldap response truncated at protocol op")
	}
	if data[pos] != 0x61 {
		return fmt.Errorf("ldap unexpected protocol op tag: %02x", data[pos])
	}
	pos += 2
	if data[pos-1]&0x80 != 0 {
		pos += int(data[pos-1] & 0x7f)
	}
	// resultCode is the first element: ENUM (0x0a) + len + value
	if pos+3 > len(data) || data[pos] != 0x0a {
		return fmt.Errorf("ldap missing resultCode")
	}
	resultCode := int(data[pos+2])
	if resultCode != 0 {
		return fmt.Errorf("ldap bind failed: resultCode=%d", resultCode)
	}
	return nil
}
