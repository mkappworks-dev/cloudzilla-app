package model

import (
	"strings"
	"time"
)

type AccessToken struct {
	ID         int64      `db:"id"           json:"id"`
	UserID     int64      `db:"user_id"      json:"user_id"`
	Name       string     `db:"name"         json:"name"`
	TokenHash  string     `db:"token_hash"   json:"-"`
	LastEight  string     `db:"last_eight"   json:"last_eight"`
	Scopes     []string   `db:"-"            json:"scopes"`
	ScopesRaw  string     `db:"scopes"       json:"-"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"   json:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"   json:"created_at"`
	// SigningKey is an SSH public key in authorized_keys form. When set, every
	// request with the token must be signed with its private key.
	SigningKey string `db:"signing_key" json:"-"`
	// Targets are the repositories ("owner/repo") and organizations ("org") the
	// token is limited to; empty for a token that isn't.
	Targets    []string `db:"-"       json:"targets,omitempty"`
	TargetsRaw string   `db:"targets" json:"-"`
}

// TargetsCover reports whether targets, the repositories ("owner/repo") and
// organizations ("org") a token is limited to, include the repo owner/repo. An
// organization covers every repository it owns.
func TargetsCover(targets []string, owner, repo string) bool {
	for _, t := range targets {
		if tOwner, tRepo, isRepo := strings.Cut(t, "/"); isRepo {
			if strings.EqualFold(tOwner, owner) && strings.EqualFold(tRepo, repo) {
				return true
			}
		} else if strings.EqualFold(t, owner) {
			return true
		}
	}
	return false
}

// SignedRequest is what a key-bound token's request carries to prove its
// holder has the private key. Signature is the base64 SSHSIG blob.
type SignedRequest struct {
	Method     string
	Target     string
	Timestamp  string
	Nonce      string
	BodySHA256 string
	Signature  string
}

// SignedRequestNamespace is the SSHSIG namespace (ssh-keygen -Y sign -n) for
// signed API requests.
const SignedRequestNamespace = "cloudzilla-api"

// Message is the text the holder signs: the request line, the time, a nonce
// and the body's hash, one per line, without a trailing newline.
func (r SignedRequest) Message() []byte {
	return []byte(strings.Join([]string{"cloudzilla-request-v1", r.Method, r.Target, r.Timestamp, r.Nonce, r.BodySHA256}, "\n"))
}
