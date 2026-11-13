package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	sshSigArmorBegin = "-----BEGIN SSH SIGNATURE-----"
	sshSigArmorEnd   = "-----END SSH SIGNATURE-----"
)

// readSSHSignature unarmors a commit's SSH signature and returns the SSHSIG blob with the public key inside it.
func readSSHSignature(armored string) ([]byte, ssh.PublicKey, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(armored), sshSigArmorBegin)
	if !ok {
		return nil, nil, errors.New("not an ssh signature")
	}
	body, ok = strings.CutSuffix(strings.TrimSpace(body), sshSigArmorEnd)
	if !ok {
		return nil, nil, errors.New("unterminated ssh signature")
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return nil, nil, err
	}
	rest, ok := bytes.CutPrefix(blob, sshsigMagic)
	if !ok {
		return nil, nil, errors.New("sshsig: missing magic")
	}
	var sig struct {
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		HashAlg   string
		Signature []byte
	}
	if err := ssh.Unmarshal(rest, &sig); err != nil {
		return nil, nil, err
	}
	pub, err := ssh.ParsePublicKey(sig.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return blob, pub, nil
}
