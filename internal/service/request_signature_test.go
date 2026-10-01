package service_test

// Integration tests for tokens bound to a signing key. They require
// TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	"golang.org/x/crypto/ssh"
)

func newSigningTokens(t *testing.T) (*service.AccessTokenService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	return service.NewAccessTokenService(store.NewAccessTokenStore(db), store.NewUserStore(db)), testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
}

func requestFor(method, target, body string, at time.Time) model.SignedRequest {
	sum := sha256.Sum256([]byte(body))
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	return model.SignedRequest{
		Method: method, Target: target, Timestamp: strconv.FormatInt(at.Unix(), 10),
		Nonce: hex.EncodeToString(nonce), BodySHA256: hex.EncodeToString(sum[:]),
	}
}

func signed(t *testing.T, signer ssh.Signer, r model.SignedRequest) model.SignedRequest {
	r.Signature = testutil.SignSSHSig(t, signer, model.SignedRequestNamespace, r.Message())
	return r
}

// A token bound to a key works only for requests its private key signed,
// recently and once, so a copy of the token string is useless on its own.
func TestVerifySignedRequest(t *testing.T) {
	svc, userID := newSigningTokens(t)
	ctx := context.Background()
	signer, pub := testutil.NewSigningKey(t)
	other, _ := testutil.NewSigningKey(t)
	_, tok, err := svc.GenerateWithKey(ctx, userID, "ci", []string{model.ScopeRepoRead}, nil, pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	base := func() model.SignedRequest {
		return requestFor("POST", "/api/repos/acme/app/collaborators", "username=bob&role=writer", now)
	}

	good := signed(t, signer, base())
	if err := svc.VerifySignedRequest(ctx, tok, good); err != nil {
		t.Fatalf("a signed request: %v", err)
	}
	if err := svc.VerifySignedRequest(ctx, tok, good); !errors.Is(err, service.ErrRequestSignature) {
		t.Errorf("the same request again: err = %v, want ErrRequestSignature", err)
	}

	resigned := func(change func(*model.SignedRequest)) model.SignedRequest {
		r := base()
		change(&r)
		return signed(t, signer, r)
	}
	tampered := func(change func(*model.SignedRequest)) model.SignedRequest {
		r := signed(t, signer, base())
		change(&r)
		return r
	}
	for name, r := range map[string]model.SignedRequest{
		"signed by another key": signed(t, other, base()),
		"another method":        tampered(func(r *model.SignedRequest) { r.Method = "DELETE" }),
		"another path":          tampered(func(r *model.SignedRequest) { r.Target = "/api/repos/acme/other/collaborators" }),
		"another body":          tampered(func(r *model.SignedRequest) { r.BodySHA256 = strings.Repeat("0", 64) }),
		"another nonce":         tampered(func(r *model.SignedRequest) { r.Nonce = strings.Repeat("a", 32) }),
		"six minutes old":       resigned(func(r *model.SignedRequest) { r.Timestamp = strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10) }),
		"six minutes ahead":     resigned(func(r *model.SignedRequest) { r.Timestamp = strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10) }),
		"a short nonce":         resigned(func(r *model.SignedRequest) { r.Nonce = "abc" }),
		"no signature":          tampered(func(r *model.SignedRequest) { r.Signature = "" }),
		"another namespace": func() model.SignedRequest {
			r := base()
			r.Signature = testutil.SignSSHSig(t, signer, "git", r.Message())
			return r
		}(),
	} {
		if err := svc.VerifySignedRequest(ctx, tok, r); !errors.Is(err, service.ErrRequestSignature) {
			t.Errorf("%s: err = %v, want ErrRequestSignature", name, err)
		}
	}
}

// Scripts sign with OpenSSH's own tool, so its signatures must verify.
func TestVerifySignedRequest_AcceptsSSHKeygenSignatures(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	svc, userID := newSigningTokens(t)
	ctx := context.Background()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if out, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	_, tok, err := svc.GenerateWithKey(ctx, userID, "ci", []string{model.ScopeRepoRead}, nil, string(pub))
	if err != nil {
		t.Fatal(err)
	}
	req := requestFor("POST", "/api/repos/acme/app/hooks", `{"url":"https://example.com"}`, time.Now())
	msg := filepath.Join(dir, "msg")
	if err := os.WriteFile(msg, req.Message(), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(keygen, "-Y", "sign", "-q", "-f", key, "-n", model.SignedRequestNamespace, msg).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y sign: %v %s", err, out)
	}
	armored, err := os.ReadFile(msg + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(armored)), "\n")
	req.Signature = strings.Join(lines[1:len(lines)-1], "")
	if err := svc.VerifySignedRequest(ctx, tok, req); err != nil {
		t.Errorf("an ssh-keygen signature: %v", err)
	}
}

// A repo:admin token skips confirmation prompts, so it must be bound to a key
// and expire soon; a scope that doesn't exist is refused.
func TestAccessTokenService_GenerateWithKey_ChecksScopesAndKeys(t *testing.T) {
	svc, userID := newSigningTokens(t)
	ctx := context.Background()
	_, pub := testutil.NewSigningKey(t)
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	weakPub, err := ssh.NewPublicKey(&weak.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	in := func(d time.Duration) *time.Time { at := time.Now().Add(d); return &at }
	admin := []string{model.ScopeRepoAdmin}

	for _, tt := range []struct {
		name    string
		scopes  []string
		expires *time.Time
		key     string
		want    error
	}{
		{"admin without a key", admin, in(24 * time.Hour), "", service.ErrAdminTokenNeedsKey},
		{"admin without expiry", admin, nil, pub, service.ErrAdminTokenNoExpiry},
		{"admin for 100 days", admin, in(100 * 24 * time.Hour), pub, service.ErrAdminTokenNoExpiry},
		{"admin with a 1024-bit RSA key", admin, in(24 * time.Hour), string(ssh.MarshalAuthorizedKey(weakPub)), service.ErrInvalidSigningKey},
		{"admin with a key that isn't one", admin, in(24 * time.Hour), "not a key", service.ErrInvalidSigningKey},
		{"admin with a key but no repositories or orgs", admin, in(30 * 24 * time.Hour), pub + " laptop", service.ErrAdminTokenNeedsTargets},
		{"unknown scope", []string{"repo"}, nil, "", service.ErrInvalidScope},
		{"read without a key", []string{model.ScopeRepoRead}, nil, "", nil},
	} {
		_, tok, err := svc.GenerateWithKey(ctx, userID, tt.name, tt.scopes, tt.expires, tt.key)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
		if err == nil && tt.key != "" && tok.SigningKey != pub {
			t.Errorf("%s: stored key %q, want it normalized to %q", tt.name, tok.SigningKey, pub)
		}
	}
}

// A key on a security key can't be copied off it. As in OpenSSH, each
// signature must also show the key was touched, unless the key was registered
// with no-touch-required.
func TestVerifySignedRequest_HardwareKeys(t *testing.T) {
	svc, userID := newSigningTokens(t)
	ctx := context.Background()
	for _, tt := range []struct {
		name         string
		noTouch, tap bool
		wantVerified bool
	}{
		{"touched", false, true, true},
		{"not touched", false, false, false},
		{"not touched, no-touch-required", true, false, true},
	} {
		key, pub := testutil.NewHardwareKey(t)
		if tt.noTouch {
			pub = "no-touch-required " + pub
		}
		_, tok, err := svc.GenerateWithKey(ctx, userID, tt.name, []string{model.ScopeRepoRead}, nil, pub)
		if err != nil {
			t.Fatalf("%s: GenerateWithKey: %v", tt.name, err)
		}
		if tt.noTouch != strings.HasPrefix(tok.SigningKey, "no-touch-required ") {
			t.Errorf("%s: stored key %q lost or invented the no-touch-required option", tt.name, tok.SigningKey)
		}
		req := requestFor("POST", "/api/repos/acme/app/keys", "title=ci", time.Now())
		req.Signature = key.SignSSHSig(req.Message(), tt.tap)
		if err := svc.VerifySignedRequest(ctx, tok, req); (err == nil) != tt.wantVerified {
			t.Errorf("%s: err = %v, want verified = %v", tt.name, err, tt.wantVerified)
		}
	}
}

// A repo:admin token is limited to repositories its creator manages and
// organizations they own, named when it's created.
func TestAccessTokenService_AdminTokensNeedTargetsTheyAdminister(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	testutil.SeedRepo(t, db, userID, owner, suffix)
	repo := owner + "/testrepo_" + suffix
	otherSuffix := testutil.UniqueSuffix(t)
	otherID := testutil.SeedUser(t, db, otherSuffix)
	testutil.SeedRepo(t, db, otherID, "testuser_"+otherSuffix, otherSuffix)
	org, err := svc.Org.Create(ctx, userID, "tokorg_"+suffix, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
	memberOf, err := svc.Org.Create(ctx, otherID, "tokorg_m_"+suffix, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, memberOf.ID) })
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, memberOf.ID, userID)
	_, key := testutil.NewSigningKey(t)
	soon := time.Now().Add(24 * time.Hour)
	admin := func(targets ...string) service.NewToken {
		return service.NewToken{Name: "ci", Scopes: []string{model.ScopeRepoAdmin}, ExpiresAt: &soon, SigningKey: key, Targets: targets}
	}

	for _, tt := range []struct {
		name string
		nt   service.NewToken
		want error
	}{
		{"no targets", admin(), service.ErrAdminTokenNeedsTargets},
		{"someone else's repository", admin("testuser_" + otherSuffix + "/testrepo_" + otherSuffix), service.ErrTokenTarget},
		{"a repository that doesn't exist", admin(owner + "/missing"), service.ErrTokenTarget},
		{"an organization that doesn't exist", admin("no_such_org_" + suffix), service.ErrTokenTarget},
		{"an organization they're only a member of", admin(memberOf.Name), service.ErrTokenTarget},
		{"targets on a token without repo:admin", service.NewToken{Name: "ci", Scopes: []string{model.ScopeRepoRead}, Targets: []string{repo}}, service.ErrTokenTarget},
	} {
		if _, err := svc.AccessToken.Check(ctx, userID, tt.nt); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}

	got, err := svc.AccessToken.Check(ctx, userID, admin(" "+repo+" ", repo, org.Name))
	if err != nil {
		t.Fatalf("own repository and organization: %v", err)
	}
	if want := []string{repo, org.Name}; !slices.Equal(got.Targets, want) {
		t.Errorf("targets = %v, want canonical and deduplicated %v", got.Targets, want)
	}
}
