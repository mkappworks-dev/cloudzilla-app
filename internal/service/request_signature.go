package service

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"golang.org/x/crypto/ssh"
)

const (
	signedRequestSkew = 5 * time.Minute
	minRSASigningBits = 2048
)

var (
	ErrRequestSignature   = errors.New("the request isn't signed with the token's key")
	ErrInvalidSigningKey  = errors.New("the signing key must be an Ed25519, ECDSA, 2048-bit RSA or hardware (sk-) SSH public key")
	ErrAdminTokenNeedsKey = errors.New("a repo:admin token needs a signing key")
)

// noTouchRequired is the authorized_keys option that lets a hardware key sign
// without a touch, as in OpenSSH.
const noTouchRequired = "no-touch-required"

// skUserPresent is the FIDO flag a hardware key sets when it was touched.
const skUserPresent = 0x01

// parseSigningKey checks key is an SSH public key fit to sign requests and
// returns it in authorized_keys form, keeping a no-touch-required option.
func parseSigningKey(key string) (string, error) {
	pub, _, options, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(key)))
	if err != nil {
		return "", ErrInvalidSigningKey
	}
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	switch pub.Type() {
	case ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
		if slices.Contains(options, noTouchRequired) {
			normalized = noTouchRequired + " " + normalized
		}
		return normalized, nil
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
	case ssh.KeyAlgoRSA:
		cpk, ok := pub.(ssh.CryptoPublicKey)
		if !ok {
			return "", ErrInvalidSigningKey
		}
		if rk, ok := cpk.CryptoPublicKey().(*rsa.PublicKey); !ok || rk.N.BitLen() < minRSASigningBits {
			return "", ErrInvalidSigningKey
		}
	default:
		return "", ErrInvalidSigningKey
	}
	return normalized, nil
}

// VerifySignedRequest checks that req, made with token, is signed by the
// token's key, recently, and for the first time.
func (s *AccessTokenService) VerifySignedRequest(ctx context.Context, token *model.AccessToken, req model.SignedRequest) error {
	pub, _, options, _, err := ssh.ParseAuthorizedKey([]byte(token.SigningKey))
	if err != nil {
		return fmt.Errorf("token %d signing key: %w", token.ID, err)
	}
	touch := !slices.Contains(options, noTouchRequired)
	ts, err := strconv.ParseInt(req.Timestamp, 10, 64)
	if err != nil {
		return ErrRequestSignature
	}
	if skew := time.Since(time.Unix(ts, 0)); skew > signedRequestSkew || skew < -signedRequestSkew {
		return ErrRequestSignature
	}
	if !validNonce(req.Nonce) {
		return ErrRequestSignature
	}
	blob, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return ErrRequestSignature
	}
	if err := verifySSHSig(pub, model.SignedRequestNamespace, req.Message(), blob, touch); err != nil {
		return ErrRequestSignature
	}
	// Kept past both sides of the skew window, so a captured request can't be sent again.
	fresh, err := s.tokens.ClaimNonce(ctx, token.ID, req.Nonce, 2*signedRequestSkew)
	if err != nil {
		return err
	}
	if !fresh {
		return ErrRequestSignature
	}
	return nil
}

func validNonce(n string) bool {
	if len(n) < 16 || len(n) > 64 {
		return false
	}
	return !strings.ContainsFunc(n, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_'
	})
}

// sshsigMagic starts both an SSHSIG blob and the data it signs; see OpenSSH's PROTOCOL.sshsig.
var sshsigMagic = []byte("SSHSIG")

// verifySSHSig checks blob, an SSHSIG signature as ssh-keygen -Y sign makes,
// over message in namespace, against pub. With touch, a hardware key's
// signature must say the key was touched.
func verifySSHSig(pub ssh.PublicKey, namespace string, message, blob []byte, touch bool) error {
	if !bytes.HasPrefix(blob, sshsigMagic) {
		return errors.New("sshsig: missing magic")
	}
	var sig struct {
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		HashAlg   string
		Signature []byte
	}
	if err := ssh.Unmarshal(blob[len(sshsigMagic):], &sig); err != nil {
		return fmt.Errorf("sshsig: %w", err)
	}
	if sig.Version != 1 || sig.Namespace != namespace || !bytes.Equal(sig.PublicKey, pub.Marshal()) {
		return errors.New("sshsig: wrong version, namespace or key")
	}
	var h hash.Hash
	switch sig.HashAlg {
	case "sha512":
		h = sha512.New()
	case "sha256":
		h = sha256.New()
	default:
		return errors.New("sshsig: unsupported hash")
	}
	h.Write(message)
	signed := append(append([]byte{}, sshsigMagic...), ssh.Marshal(struct {
		Namespace string
		Reserved  string
		HashAlg   string
		Hash      []byte
	}{namespace, "", sig.HashAlg, h.Sum(nil)})...)
	var s ssh.Signature
	if err := ssh.Unmarshal(sig.Signature, &s); err != nil {
		return fmt.Errorf("sshsig: signature: %w", err)
	}
	if err := pub.Verify(signed, &s); err != nil {
		return err
	}
	// x/crypto checks a hardware key's signature, not its user-presence flag.
	if isHardwareKey(pub) && touch && (len(s.Rest) == 0 || s.Rest[0]&skUserPresent == 0) {
		return errors.New("sshsig: the hardware key wasn't touched")
	}
	return nil
}

func isHardwareKey(pub ssh.PublicKey) bool {
	return pub.Type() == ssh.KeyAlgoSKED25519 || pub.Type() == ssh.KeyAlgoSKECDSA256
}
