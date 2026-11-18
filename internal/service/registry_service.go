package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	registryTokenTTL  = 5 * time.Minute
	registryTokenType = "registry"
	// Mixed into the signing key so a registry token can't verify as a session JWT, which shares auth.jwt_secret.
	registryKeyPurpose = "cloudzilla.registry-token.v1"
)

// ErrRegistryCredentials is a missing or wrong credential; the token endpoint answers it with a Basic challenge.
var ErrRegistryCredentials = errors.New("invalid registry credentials")

// ErrRegistryToken is a registry token that is malformed, tampered with, expired or for another service.
var ErrRegistryToken = errors.New("invalid registry token")

// RegistryForbiddenError is a credential that is valid but may not use the registry; Reason names why.
type RegistryForbiddenError struct{ Reason string }

func (e *RegistryForbiddenError) Error() string { return e.Reason }

// RegistryScope is one requested "repository:<name>:<actions>" scope.
type RegistryScope struct {
	Name    string
	Actions []string
}

// ParseRegistryScope reads "repository:<name>:<actions>"; any other resource type reports false.
func ParseRegistryScope(s string) (RegistryScope, bool) {
	rest, ok := strings.CutPrefix(s, "repository:")
	if !ok {
		return RegistryScope{}, false
	}
	i := strings.LastIndex(rest, ":")
	if i <= 0 {
		return RegistryScope{}, false
	}
	var actions []string
	for _, a := range strings.Split(rest[i+1:], ",") {
		if (a == RegistryActionPull || a == RegistryActionPush || a == RegistryActionDelete) && !slices.Contains(actions, a) {
			actions = append(actions, a)
		}
	}
	return RegistryScope{Name: rest[:i], Actions: actions}, true
}

// RegistryAccess is one granted entry of a token's access list.
type RegistryAccess struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

// RegistryClaims is what a verified registry token says; UserID is 0 for an anonymous caller.
type RegistryClaims struct {
	UserID   int64
	Username string
	Access   []RegistryAccess
}

// Allows reports whether the token grants action on name.
func (c *RegistryClaims) Allows(name, action string) bool {
	return slices.ContainsFunc(c.Access, func(a RegistryAccess) bool {
		return a.Type == "repository" && a.Name == name && slices.Contains(a.Actions, action)
	})
}

// RegistryToken is the token endpoint's answer.
type RegistryToken struct {
	Token     string
	ExpiresIn int
	IssuedAt  time.Time
}

type registryJWTClaims struct {
	jwt.RegisteredClaims
	Type     string           `json:"typ"`
	Username string           `json:"username,omitempty"`
	Access   []RegistryAccess `json:"access"`
}

// RegistryService is the OCI registry's logic: token issue and verify, upload sessions, blobs, manifests, tags.
type RegistryService struct {
	registry *store.RegistryStore
	packages *PackageService
	cfg      config.RegistryConfig
	now      func() time.Time
	backend  storage.Backend
	spoolDir string

	tokens  *AccessTokenService
	repos   *RepoService
	key     []byte
	realm   string
	service string
}

func NewRegistryService(registry *store.RegistryStore, packages *PackageService, cfg config.RegistryConfig) *RegistryService {
	return &RegistryService{registry: registry, packages: packages, cfg: cfg, now: time.Now}
}

// WithAuth gives the service what the token endpoint checks credentials and access against. baseURL fixes the
// token realm and the service name clients echo back.
func (s *RegistryService) WithAuth(tokens *AccessTokenService, repos *RepoService, jwtSecret, baseURL string) *RegistryService {
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte(registryKeyPurpose))
	s.tokens, s.repos, s.key = tokens, repos, mac.Sum(nil)
	s.realm = strings.TrimRight(baseURL, "/") + "/v2/token"
	if u, err := url.Parse(baseURL); err == nil {
		s.service = u.Host
	}
	return s
}

// WithClock replaces the clock token expiry is judged against.
func (s *RegistryService) WithClock(now func() time.Time) *RegistryService {
	s.now = now
	return s
}

// Realm is the token endpoint URL a challenge points clients at.
func (s *RegistryService) Realm() string { return s.realm }

// Service is the service name in challenges and in the token's audience.
func (s *RegistryService) Service() string { return s.service }

// IssueToken checks credentials and returns a token whose access list is the requested scopes intersected with what
// the caller may do. rawPAT is empty for an anonymous caller, who may ask only for pull on public images.
func (s *RegistryService) IssueToken(ctx context.Context, rawPAT string, requested []RegistryScope) (*RegistryToken, error) {
	var user *model.User
	var capability []string
	if rawPAT != "" {
		var err error
		if user, capability, err = s.authenticate(ctx, rawPAT); err != nil {
			return nil, err
		}
	}
	var access []RegistryAccess
	for _, want := range requested {
		granted, err := s.grant(ctx, user, capability, want)
		if err != nil {
			return nil, err
		}
		if user == nil && len(granted) < len(want.Actions) {
			return nil, ErrRegistryCredentials
		}
		if len(granted) > 0 {
			access = append(access, RegistryAccess{Type: "repository", Name: want.Name, Actions: granted})
		}
	}
	return s.sign(user, access)
}

// authenticate accepts a personal access token only, never an account password, which would skip two-factor,
// suspension and SSO. capability is the registry actions the token's scopes allow.
func (s *RegistryService) authenticate(ctx context.Context, rawPAT string) (*model.User, []string, error) {
	if !strings.HasPrefix(rawPAT, "czp_") {
		return nil, nil, ErrRegistryCredentials
	}
	tok, user, err := s.tokens.Validate(ctx, rawPAT)
	switch {
	case errors.Is(err, model.ErrAccountSuspended):
		return nil, nil, &RegistryForbiddenError{Reason: "this account is suspended"}
	case err != nil:
		return nil, nil, ErrRegistryCredentials
	case tok.SigningKey != "":
		return nil, nil, &RegistryForbiddenError{Reason: "a token bound to a signing key can't be used with the registry"}
	}
	var capability []string
	if slices.Contains(tok.Scopes, model.ScopePackagesWrite) {
		capability = []string{RegistryActionPull, RegistryActionPush, RegistryActionDelete}
	} else if slices.Contains(tok.Scopes, model.ScopePackagesRead) {
		capability = []string{RegistryActionPull}
	}
	if capability == nil {
		return nil, nil, &RegistryForbiddenError{Reason: "the token needs the packages:read or packages:write scope"}
	}
	if err := s.tokens.UpdateLastUsed(ctx, tok.ID); err != nil {
		slog.Warn("registry token last_used update failed", "token_id", tok.ID, "error", err)
	}
	return user, capability, nil
}

// grant returns the requested actions the caller may take on want.Name, in pull, push, delete order.
func (s *RegistryService) grant(ctx context.Context, user *model.User, capability []string, want RegistryScope) ([]string, error) {
	may, err := s.mayDo(ctx, user, want.Name)
	if err != nil || len(may) == 0 {
		return nil, err
	}
	var granted []string
	for _, a := range []string{RegistryActionPull, RegistryActionPush, RegistryActionDelete} {
		if slices.Contains(want.Actions, a) && slices.Contains(may, a) && (user == nil || slices.Contains(capability, a)) {
			granted = append(granted, a)
		}
	}
	return granted, nil
}

// mayDo is every action the caller's repository permissions allow on the image name.
func (s *RegistryService) mayDo(ctx context.Context, user *model.User, name string) ([]string, error) {
	segment, image, ok := strings.Cut(name, "/")
	if !ok || !ValidRegistryName(name) {
		return nil, nil
	}
	owner, err := s.packages.packages.ResolveOwner(ctx, segment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var userID *int64
	if user != nil {
		userID = &user.ID
	}
	repoID, err := s.packages.packages.LinkedRepoID(ctx, owner.UserID, owner.OrgID, image)
	switch {
	case err == nil:
		repo, err := s.repos.GetByID(ctx, repoID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var may []string
		if s.repos.CanRead(ctx, repo, userID) {
			may = append(may, RegistryActionPull)
		}
		if user != nil && s.repos.CanWrite(ctx, repo, user.ID) {
			may = append(may, RegistryActionPush, RegistryActionDelete)
		}
		return may, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	// No linked package: a writer may start one. Pull is granted too because docker asks for both together and
	// its first request is a HEAD of a blob; the registry answers it from what exists, which is nothing.
	if user == nil {
		return nil, nil
	}
	pending, err := s.packages.packages.HasPending(ctx, owner.UserID, owner.OrgID, image, user.ID, s.now())
	if err != nil {
		return nil, err
	}
	if !pending {
		if pending, err = s.writesAnyOf(ctx, owner, user.ID); err != nil || !pending {
			return nil, err
		}
	}
	return []string{RegistryActionPull, RegistryActionPush}, nil
}

func (s *RegistryService) writesAnyOf(ctx context.Context, owner store.PathOwner, userID int64) (bool, error) {
	repos, err := s.repos.ListByOwner(ctx, owner.Name)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(repos, func(r model.Repository) bool { return s.repos.CanWrite(ctx, &r, userID) }), nil
}

func (s *RegistryService) sign(user *model.User, access []RegistryAccess) (*RegistryToken, error) {
	issued := s.now()
	claims := registryJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.service,
			Audience:  jwt.ClaimStrings{s.service},
			IssuedAt:  jwt.NewNumericDate(issued),
			ExpiresAt: jwt.NewNumericDate(issued.Add(registryTokenTTL)),
		},
		Type:   registryTokenType,
		Access: access,
	}
	if claims.Access == nil {
		claims.Access = []RegistryAccess{}
	}
	if user != nil {
		claims.Subject, claims.Username = strconv.FormatInt(user.ID, 10), user.Username
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		return nil, fmt.Errorf("sign registry token: %w", err)
	}
	return &RegistryToken{Token: signed, ExpiresIn: int(registryTokenTTL / time.Second), IssuedAt: issued}, nil
}

// VerifyToken checks a registry token's signature, audience, type and expiry against the service clock.
func (s *RegistryService) VerifyToken(raw string) (*RegistryClaims, error) {
	var claims registryJWTClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return s.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithAudience(s.service),
		jwt.WithTimeFunc(s.now), jwt.WithExpirationRequired())
	if err != nil || claims.Type != registryTokenType {
		return nil, ErrRegistryToken
	}
	out := &RegistryClaims{Username: claims.Username, Access: claims.Access}
	if claims.Subject != "" {
		id, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil {
			return nil, ErrRegistryToken
		}
		out.UserID = id
	}
	return out, nil
}
