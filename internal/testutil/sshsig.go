package testutil

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"golang.org/x/crypto/ssh"
)

// NewSigningKey returns a fresh Ed25519 signer and its public key in
// authorized_keys form, for tokens bound to a key.
func NewSigningKey(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
}

// SignSSHSig returns the base64 SSHSIG blob of message in namespace, as
// ssh-keygen -Y sign makes it.
func SignSSHSig(t *testing.T, signer ssh.Signer, namespace string, message []byte) string {
	t.Helper()
	sum := sha512.Sum512(message)
	signed := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Namespace, Reserved, HashAlg string
		Hash                         []byte
	}{namespace, "", "sha512", sum[:]})...)
	sig, err := signer.Sign(rand.Reader, signed)
	if err != nil {
		t.Fatal(err)
	}
	blob := append([]byte("SSHSIG"), ssh.Marshal(struct {
		Version                      uint32
		PublicKey                    []byte
		Namespace, Reserved, HashAlg string
		Signature                    []byte
	}{1, signer.PublicKey().Marshal(), namespace, "", "sha512", ssh.Marshal(sig)})...)
	return base64.StdEncoding.EncodeToString(blob)
}

// SignHTTPRequest signs req for a token bound to signer's key, as a script
// would: it sets the timestamp, a fresh nonce and the signature headers.
func SignHTTPRequest(t *testing.T, signer ssh.Signer, req *http.Request) {
	t.Helper()
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			t.Fatal(err)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sr := model.SignedRequest{
		Method:     req.Method,
		Target:     req.URL.RequestURI(),
		Timestamp:  strconv.FormatInt(time.Now().Unix(), 10),
		Nonce:      hex.EncodeToString(nonce),
		BodySHA256: hex.EncodeToString(sum[:]),
	}
	req.Header.Set("X-Cloudzilla-Timestamp", sr.Timestamp)
	req.Header.Set("X-Cloudzilla-Nonce", sr.Nonce)
	req.Header.Set("X-Cloudzilla-Signature", SignSSHSig(t, signer, model.SignedRequestNamespace, sr.Message()))
}
