package service

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"golang.org/x/crypto/ssh"
)

type SignatureStatus string

const (
	SignatureUnsigned          SignatureStatus = "unsigned"
	SignatureVerified          SignatureStatus = "verified"
	SignaturePartiallyVerified SignatureStatus = "partially_verified"
	SignatureUnverified        SignatureStatus = "unverified"
)

type SignatureReason string

const (
	ReasonUnsupported        SignatureReason = "unsupported"
	ReasonNoKey              SignatureReason = "no_key"
	ReasonBadSignature       SignatureReason = "bad_signature"
	ReasonKeyExpired         SignatureReason = "key_expired"
	ReasonKeyRevoked         SignatureReason = "key_revoked"
	ReasonEmailNotVerified   SignatureReason = "email_not_verified"
	ReasonEmailNotOnKey      SignatureReason = "email_not_on_key"
	ReasonMultipleSignatures SignatureReason = "multiple_signatures"
)

type SignatureKind string

const (
	SignatureKindGPG SignatureKind = "gpg"
	SignatureKindSSH SignatureKind = "ssh"
)

// SignatureResult is what a page needs to badge a commit; it carries no go-git or crypto types.
type SignatureResult struct {
	Status         SignatureStatus
	Reason         SignatureReason
	Kind           SignatureKind
	KeyLabel       string
	Fingerprint    string
	KeyID          string
	SignerUsername string
	CommitterEmail string
}

type gpgKeyLookup interface {
	ListByKeyIDs(ctx context.Context, keyIDs []string) ([]model.GPGKey, error)
}

type sshSigningKeyLookup interface {
	ListByFingerprints(ctx context.Context, fingerprints []string) ([]model.SSHSigningKey, error)
}

type signerDirectory interface {
	SigningIdentities(ctx context.Context, userIDs []int64) (map[int64]SigningIdentity, error)
}

// SignatureService decides, on every read, whether a commit's signature is made by a key registered to the account
// whose email the commit carries. Nothing is cached: adding or deleting a key changes the next render.
type SignatureService struct {
	gpg  gpgKeyLookup
	ssh  sshSigningKeyLookup
	dirs signerDirectory
}

func NewSignatureService(gpg gpgKeyLookup, sshKeys sshSigningKeyLookup, dirs signerDirectory) *SignatureService {
	return &SignatureService{gpg: gpg, ssh: sshKeys, dirs: dirs}
}

// pendingSignature is a signed commit that needs a key lookup before it can be judged.
type pendingSignature struct {
	commit  *object.Commit
	payload []byte
	// GPG
	sig   *packet.Signature
	keyID string
	// SSH
	blob []byte
	pub  ssh.PublicKey
}

// Verify judges each commit with a constant number of queries: one per key kind, one for the owners. A commit that
// cannot be judged is unverified, never an error; the error is only for a failed lookup.
func (s *SignatureService) Verify(ctx context.Context, commits []*object.Commit) (map[plumbing.Hash]SignatureResult, error) {
	out := make(map[plumbing.Hash]SignatureResult, len(commits))
	var gpgPending, sshPending []pendingSignature
	for _, c := range commits {
		if _, done := out[c.Hash]; done {
			continue
		}
		res, p := classify(c)
		out[c.Hash] = res
		switch {
		case p == nil:
		case p.sig != nil:
			gpgPending = append(gpgPending, *p)
		default:
			sshPending = append(sshPending, *p)
		}
	}

	gpgKeys, err := s.lookupGPG(ctx, gpgPending)
	if err != nil {
		return nil, err
	}
	sshKeys, err := s.lookupSSH(ctx, sshPending)
	if err != nil {
		return nil, err
	}

	var owners []int64
	for _, k := range gpgKeys {
		owners = append(owners, k.UserID)
	}
	for _, k := range sshKeys {
		owners = append(owners, k.UserID)
	}
	var identities map[int64]SigningIdentity
	if len(owners) > 0 {
		slices.Sort(owners)
		owners = slices.Compact(owners)
		if identities, err = s.dirs.SigningIdentities(ctx, owners); err != nil {
			return nil, err
		}
	}

	for _, p := range gpgPending {
		out[p.commit.Hash] = judgeGPG(p, gpgKeys, identities)
	}
	for _, p := range sshPending {
		out[p.commit.Hash] = judgeSSH(p, sshKeys, identities)
	}
	return out, nil
}

func (s *SignatureService) lookupGPG(ctx context.Context, pending []pendingSignature) ([]model.GPGKey, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(pending))
	for _, p := range pending {
		ids = append(ids, p.keyID)
	}
	slices.Sort(ids)
	return s.gpg.ListByKeyIDs(ctx, slices.Compact(ids))
}

func (s *SignatureService) lookupSSH(ctx context.Context, pending []pendingSignature) ([]model.SSHSigningKey, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	fps := make([]string, 0, len(pending))
	for _, p := range pending {
		fps = append(fps, ssh.FingerprintSHA256(p.pub))
	}
	slices.Sort(fps)
	return s.ssh.ListByFingerprints(ctx, slices.Compact(fps))
}

func unverified(c *object.Commit, reason SignatureReason) SignatureResult {
	return SignatureResult{Status: SignatureUnverified, Reason: reason, CommitterEmail: c.Committer.Email}
}

// classify settles what needs no key: unsigned, unsupported and multiply-signed commits. Anything else comes back
// with the data its lookup needs.
func classify(c *object.Commit) (SignatureResult, *pendingSignature) {
	raw := strings.TrimSpace(c.PGPSignature)
	if raw == "" {
		return SignatureResult{Status: SignatureUnsigned, CommitterEmail: c.Committer.Email}, nil
	}
	if strings.Count(raw, "-----BEGIN ") > 1 {
		return unverified(c, ReasonMultipleSignatures), nil
	}
	payload, err := signedPayload(c)
	if err != nil {
		return unverified(c, ReasonUnsupported), nil
	}
	p := &pendingSignature{commit: c, payload: payload}
	switch {
	case strings.HasPrefix(raw, "-----BEGIN PGP SIGNATURE-----"):
		sig, err := readPGPSignature(raw)
		if err != nil {
			return unverified(c, ReasonUnsupported), nil
		}
		id, ok := issuerKeyID(sig)
		if !ok {
			return unverified(c, ReasonUnsupported), nil
		}
		p.sig, p.keyID = sig, id
	case strings.HasPrefix(raw, "-----BEGIN SSH SIGNATURE-----"):
		blob, pub, err := readSSHSignature(raw)
		if err != nil {
			return unverified(c, ReasonUnsupported), nil
		}
		p.blob, p.pub = blob, pub
	default:
		return unverified(c, ReasonUnsupported), nil
	}
	return SignatureResult{}, p
}

func signedPayload(c *object.Commit) ([]byte, error) {
	obj := &plumbing.MemoryObject{}
	if err := c.EncodeWithoutSignature(obj); err != nil {
		return nil, err
	}
	r, err := obj.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func readPGPSignature(armored string) (*packet.Signature, error) {
	block, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		return nil, err
	}
	pkt, err := packet.Read(block.Body)
	if err != nil {
		return nil, err
	}
	sig, ok := pkt.(*packet.Signature)
	if !ok {
		return nil, fmt.Errorf("not a signature packet")
	}
	return sig, nil
}

// issuerKeyID prefers the issuer subpacket, falling back to the tail of the issuer fingerprint that newer signatures
// carry alone.
func issuerKeyID(sig *packet.Signature) (string, bool) {
	if sig.IssuerKeyId != nil {
		return fmt.Sprintf("%016x", *sig.IssuerKeyId), true
	}
	if n := len(sig.IssuerFingerprint); n >= 8 {
		return fmt.Sprintf("%x", sig.IssuerFingerprint[n-8:]), true
	}
	return "", false
}

func judgeGPG(p pendingSignature, keys []model.GPGKey, identities map[int64]SigningIdentity) SignatureResult {
	committer := p.commit.Committer
	res := SignatureResult{Kind: SignatureKindGPG, KeyID: p.keyID, CommitterEmail: committer.Email}
	found := false
	for i := range keys {
		key := &keys[i]
		matchedSub := -1
		switch {
		case key.KeyID == p.keyID:
		default:
			for j, sk := range key.Subkeys {
				if sk.KeyID == p.keyID && sk.CanSign {
					matchedSub = j
				}
			}
			if matchedSub < 0 {
				continue
			}
		}
		pub, ok := gpgPublicKey(key, p.keyID)
		if !ok {
			continue
		}
		found = true
		if !verifyPGP(pub, p.sig, p.payload) {
			continue
		}

		res.Fingerprint, res.KeyLabel = key.Fingerprint, key.KeyID
		if matchedSub >= 0 {
			res.Fingerprint = key.Subkeys[matchedSub].Fingerprint
		}
		id, ok := identities[key.UserID]
		if !ok {
			res.Status, res.Reason = SignatureUnverified, ReasonNoKey
			return res
		}
		res.SignerUsername = id.Username

		if reason, bad := gpgKeyState(key, matchedSub, committer.When); bad {
			res.Status, res.Reason = SignatureUnverified, reason
			return res
		}
		return emailVerdict(res, id, key.UserID, committer.Email, key.Emails, true)
	}
	res.Status = SignatureUnverified
	res.Reason = ReasonNoKey
	if found {
		res.Reason = ReasonBadSignature
	}
	return res
}

// gpgPublicKey finds the packet for keyID in the stored armored key.
func gpgPublicKey(key *model.GPGKey, keyID string) (*packet.PublicKey, bool) {
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key.PublicKey))
	if err != nil || len(ring) != 1 {
		return nil, false
	}
	e := ring[0]
	if fmt.Sprintf("%016x", e.PrimaryKey.KeyId) == keyID {
		return e.PrimaryKey, true
	}
	for _, sk := range e.Subkeys {
		if fmt.Sprintf("%016x", sk.PublicKey.KeyId) == keyID {
			return sk.PublicKey, true
		}
	}
	return nil, false
}

// verifyPGP checks the signature itself and leaves expiry and revocation to the caller: go-crypto's keyring check
// judges them at the current time, which would reject a commit signed before its key expired.
func verifyPGP(pub *packet.PublicKey, sig *packet.Signature, payload []byte) bool {
	if sig.SigType != packet.SigTypeBinary && sig.SigType != packet.SigTypeText {
		return false
	}
	h, err := sig.PrepareVerify()
	if err != nil {
		return false
	}
	_, _ = h.Write(payload)
	return pub.VerifySignature(h, sig) == nil
}

func gpgKeyState(key *model.GPGKey, sub int, at time.Time) (SignatureReason, bool) {
	revoked := key.RevokedAt != nil
	expiry := []*time.Time{key.ExpiresAt}
	if sub >= 0 {
		revoked = revoked || key.Subkeys[sub].RevokedAt != nil
		expiry = append(expiry, key.Subkeys[sub].ExpiresAt)
	}
	if revoked {
		return ReasonKeyRevoked, true
	}
	for _, e := range expiry {
		if e != nil && !at.Before(*e) {
			return ReasonKeyExpired, true
		}
	}
	return "", false
}

func judgeSSH(p pendingSignature, keys []model.SSHSigningKey, identities map[int64]SigningIdentity) SignatureResult {
	committer := p.commit.Committer
	fp := ssh.FingerprintSHA256(p.pub)
	res := SignatureResult{Kind: SignatureKindSSH, Fingerprint: fp, CommitterEmail: committer.Email}
	found := false
	for _, key := range keys {
		if key.Fingerprint != fp {
			continue
		}
		stored, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
		if err != nil {
			continue
		}
		found = true
		// A hardware key's touch flag is not required: ssh-keygen -Y sign can be told to skip it.
		if verifySSHSig(stored, "git", p.payload, p.blob, false) != nil {
			continue
		}
		res.KeyLabel = key.Title
		id, ok := identities[key.UserID]
		if !ok {
			res.Status, res.Reason = SignatureUnverified, ReasonNoKey
			return res
		}
		res.SignerUsername = id.Username
		return emailVerdict(res, id, key.UserID, committer.Email, nil, false)
	}
	res.Status = SignatureUnverified
	res.Reason = ReasonNoKey
	if found {
		res.Reason = ReasonBadSignature
	}
	return res
}

// emailVerdict applies the committer email rule. Only a GPG key (checkKey) lists UID emails; an SSH key has none.
func emailVerdict(res SignatureResult, id SigningIdentity, userID int64, email string, keyEmails []string, checkKey bool) SignatureResult {
	if !emailVerified(id, userID, email) {
		res.Status, res.Reason = SignaturePartiallyVerified, ReasonEmailNotVerified
		return res
	}
	if checkKey && !slices.Contains(keyEmails, strings.ToLower(email)) {
		res.Status, res.Reason = SignaturePartiallyVerified, ReasonEmailNotOnKey
		return res
	}
	res.Status = SignatureVerified
	return res
}

// emailVerified also accepts a noreply address minted under an earlier base URL, as userByAuthorEmail does.
func emailVerified(id SigningIdentity, userID int64, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if slices.Contains(id.VerifiedEmails, email) {
		return true
	}
	nid, name, ok := parseNoreplyEmail(email)
	return ok && nid == userID && strings.EqualFold(name, id.Username)
}
