package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
	"golang.org/x/crypto/ssh"
)

const (
	instanceSigningPurpose = "instance-signing"
	// sshSigNamespace is the namespace git passes to ssh-keygen -Y sign.
	sshSigNamespace = "git"
	// sshSigLineLen is the base64 line width of an armored SSHSIG, as ssh-keygen writes it.
	sshSigLineLen = 70
)

// ErrNoSecretKey means the server can't seal an instance signing key because security.secret_key is not set.
var ErrNoSecretKey = errors.New("instance signing needs security.secret_key")

type instanceKeyStore interface {
	Active(ctx context.Context) (*model.InstanceSigningKey, error)
	CreateIfNoActive(ctx context.Context, key *model.InstanceSigningKey) (bool, error)
	Rotate(ctx context.Context, key *model.InstanceSigningKey) error
}

// InstanceSigner holds the key the server signs its own commits with. The private half is sealed under
// security.secret_key and generated the first time something needs to sign.
type InstanceSigner struct {
	keys instanceKeyStore
	box  *secretbox.Box
}

// NewInstanceSigner returns a signer; a nil box makes every operation fail with ErrNoSecretKey.
func NewInstanceSigner(keys instanceKeyStore, box *secretbox.Box) *InstanceSigner {
	return &InstanceSigner{keys: keys, box: box}
}

// InstanceKeyState is what the admin page shows about the signing key.
type InstanceKeyState struct {
	// Sealable is false when security.secret_key is unset.
	Sealable bool
	// Key is the active key, nil when none has been generated.
	Key *model.InstanceSigningKey
	// Unreadable means the active key was sealed under a different security.secret_key.
	Unreadable bool
}

// State reports the active key without generating one.
func (s *InstanceSigner) State(ctx context.Context) (InstanceKeyState, error) {
	key, err := s.keys.Active(ctx)
	if err != nil {
		return InstanceKeyState{}, err
	}
	st := InstanceKeyState{Sealable: s.box != nil, Key: key}
	if key != nil && st.Sealable {
		_, err := s.box.Open(instanceSigningPurpose, key.SealedPrivate)
		st.Unreadable = errors.Is(err, secretbox.ErrUndecryptable)
	}
	return st, nil
}

// Sign returns the armored SSHSIG, namespace git, that git stores in a commit's gpgsig header.
func (s *InstanceSigner) Sign(ctx context.Context, payload []byte) (string, error) {
	if s.box == nil {
		return "", ErrNoSecretKey
	}
	key, err := s.keys.Active(ctx)
	if err != nil {
		return "", err
	}
	if key == nil {
		if key, err = s.generateFirst(ctx); err != nil {
			return "", err
		}
	}
	seed, err := s.box.Open(instanceSigningPurpose, key.SealedPrivate)
	if err != nil {
		return "", fmt.Errorf("open instance signing key %d: %w", key.ID, err)
	}
	signer, err := ssh.NewSignerFromKey(ed25519.PrivateKey(seed))
	if err != nil {
		return "", fmt.Errorf("instance signing key %d: %w", key.ID, err)
	}
	return signSSHSig(signer, sshSigNamespace, payload)
}

// Rotate retires the active key and makes a new one active.
func (s *InstanceSigner) Rotate(ctx context.Context) (*model.InstanceSigningKey, error) {
	if s.box == nil {
		return nil, ErrNoSecretKey
	}
	key, err := s.newKey()
	if err != nil {
		return nil, err
	}
	if err := s.keys.Rotate(ctx, key); err != nil {
		return nil, err
	}
	return key, nil
}

// generateFirst stores a new key unless a concurrent caller stored one first, in which case that key wins.
func (s *InstanceSigner) generateFirst(ctx context.Context) (*model.InstanceSigningKey, error) {
	key, err := s.newKey()
	if err != nil {
		return nil, err
	}
	created, err := s.keys.CreateIfNoActive(ctx, key)
	if err != nil {
		return nil, err
	}
	if created {
		return key, nil
	}
	if key, err = s.keys.Active(ctx); err != nil || key == nil {
		return nil, fmt.Errorf("instance signing key vanished after a concurrent create: %w", err)
	}
	return key, nil
}

func (s *InstanceSigner) newKey() (*model.InstanceSigningKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate instance signing key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	sealed, err := s.box.Seal(instanceSigningPurpose, priv)
	if err != nil {
		return nil, fmt.Errorf("seal instance signing key: %w", err)
	}
	return &model.InstanceSigningKey{
		PublicKey:     strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
		Fingerprint:   ssh.FingerprintSHA256(sshPub),
		SealedPrivate: sealed,
	}, nil
}

// signSSHSig produces the armored SSHSIG of PROTOCOL.sshsig that ssh-keygen -Y sign writes.
func signSSHSig(signer ssh.Signer, namespace string, message []byte) (string, error) {
	const hashAlg = "sha512"
	digest := sha512.Sum512(message)
	signed := append(append([]byte{}, sshsigMagic...), ssh.Marshal(struct {
		Namespace string
		Reserved  string
		HashAlg   string
		Hash      []byte
	}{namespace, "", hashAlg, digest[:]})...)
	sig, err := signer.Sign(rand.Reader, signed)
	if err != nil {
		return "", err
	}
	blob := append(append([]byte{}, sshsigMagic...), ssh.Marshal(struct {
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		HashAlg   string
		Signature []byte
	}{1, signer.PublicKey().Marshal(), namespace, "", hashAlg, ssh.Marshal(sig)})...)

	enc := base64.StdEncoding.EncodeToString(blob)
	var b bytes.Buffer
	b.WriteString(sshSigArmorBegin + "\n")
	for len(enc) > sshSigLineLen {
		b.WriteString(enc[:sshSigLineLen] + "\n")
		enc = enc[sshSigLineLen:]
	}
	b.WriteString(enc + "\n" + sshSigArmorEnd)
	return b.String(), nil
}
