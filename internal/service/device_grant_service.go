package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// Poll outcomes are RFC 8628 §3.5 error codes; the handler sends err.Error() as the body.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrExpiredToken         = errors.New("expired_token")
	ErrAccessDenied         = errors.New("access_denied")
	ErrInvalidDeviceGrant   = errors.New("invalid_grant")

	ErrDeviceGrantNotFound = errors.New("device grant not found")
	ErrTooManyDeviceGrants = errors.New("too many pending device logins")
)

const (
	DeviceGrantTTL     = 15 * time.Minute
	deviceInterval     = 5
	maxLiveGrantsPerIP = 5
	userCodeAlphabet   = "BCDFGHJKLMNPQRSTVWXZ"
	userCodeLen        = 8
	deviceNameMax      = 40
	staleGrantAge      = 24 * time.Hour
	userCodeAttempts   = 3
	defaultDeviceName  = "cz CLI"
)

// DeviceCode is what a client gets when it asks to log in.
type DeviceCode struct {
	DeviceCode string
	UserCode   string
	ExpiresIn  int
	Interval   int
}

// DeviceToken is the result of a successful poll; AccessToken is shown once.
type DeviceToken struct {
	AccessToken string
	Scopes      []string
}

// DeviceGrantService runs the device-code login: the CLI asks for a code, a signed-in
// user approves it in the browser, and the CLI's next poll turns it into a personal access token.
type DeviceGrantService struct {
	grants *store.DeviceGrantStore
	tokens *store.AccessTokenStore
	now    func() time.Time
}

func NewDeviceGrantService(grants *store.DeviceGrantStore, tokens *store.AccessTokenStore) *DeviceGrantService {
	return &DeviceGrantService{grants: grants, tokens: tokens, now: time.Now}
}

// SetClock replaces the time source, for tests.
func (s *DeviceGrantService) SetClock(now func() time.Time) { s.now = now }

// Create starts a login for the client at ip. Scopes default to repo:write; repo:admin and
// unknown scopes are ErrInvalidScope.
func (s *DeviceGrantService) Create(ctx context.Context, ip string, scopes []string, deviceName string) (*DeviceCode, error) {
	if len(scopes) == 0 {
		scopes = []string{model.ScopeRepoWrite}
	}
	scopes, err := validateScopes(scopes)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.grants.DeleteStale(ctx, now.Add(-staleGrantAge)); err != nil {
		slog.Warn("delete stale device grants", "error", err)
	}

	deviceCode, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	g := &model.DeviceGrant{
		DeviceCodeHash: hashDeviceCode(deviceCode),
		Scopes:         scopes,
		DeviceName:     cleanDeviceName(deviceName),
		RequesterIP:    ip,
		IntervalSecs:   deviceInterval,
		ExpiresAt:      now.Add(DeviceGrantTTL),
	}
	for attempt := 0; ; attempt++ {
		if g.UserCode, err = newUserCode(); err != nil {
			return nil, err
		}
		err = s.grants.Create(ctx, g, maxLiveGrantsPerIP, now)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && attempt < userCodeAttempts {
			continue
		}
		break
	}
	if errors.Is(err, store.ErrTooManyGrants) {
		return nil, ErrTooManyDeviceGrants
	}
	if err != nil {
		return nil, err
	}
	return &DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   g.UserCode[:4] + "-" + g.UserCode[4:],
		ExpiresIn:  int(DeviceGrantTTL.Seconds()),
		Interval:   deviceInterval,
	}, nil
}

// Poll answers the client's token request: a poll error, or the token on the first poll after approval.
func (s *DeviceGrantService) Poll(ctx context.Context, deviceCode string) (*DeviceToken, error) {
	now := s.now()
	hash := hashDeviceCode(deviceCode)
	g, err := s.grants.GetByDeviceHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) || err == nil && g.Status == model.DeviceGrantConsumed {
		return nil, ErrInvalidDeviceGrant
	}
	if err != nil {
		return nil, err
	}
	tooFast, err := s.grants.Touch(ctx, g.ID, now)
	if err != nil {
		return nil, err
	}
	switch {
	case tooFast:
		return nil, ErrSlowDown
	case !now.Before(g.ExpiresAt):
		return nil, ErrExpiredToken
	case g.Status == model.DeviceGrantPending:
		return nil, ErrAuthorizationPending
	case g.Status == model.DeviceGrantDenied:
		return nil, ErrAccessDenied
	}

	var raw string
	err = s.grants.Redeem(ctx, hash, now, func(ctx context.Context, tx *sql.Tx, g *model.DeviceGrant) error {
		var t *model.AccessToken
		var err error
		raw, t, err = newToken(g.UserID.Int64, NewToken{Name: tokenName(g.DeviceName, now), Scopes: g.Scopes})
		if err != nil {
			return err
		}
		return s.tokens.CreateTx(ctx, tx, t)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidDeviceGrant
	}
	if err != nil {
		return nil, err
	}
	return &DeviceToken{AccessToken: raw, Scopes: g.Scopes}, nil
}

// Lookup returns the pending grant for a code as the user typed it.
func (s *DeviceGrantService) Lookup(ctx context.Context, userCode string) (*model.DeviceGrant, error) {
	code, ok := normalizeUserCode(userCode)
	if !ok {
		return nil, ErrDeviceGrantNotFound
	}
	g, err := s.grants.GetPendingByUserCode(ctx, code, s.now())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceGrantNotFound
	}
	return g, err
}

// Approve lets userID release the grant with scopes, which must be a non-empty subset of
// what the client asked for: the page can narrow a request, never widen it.
func (s *DeviceGrantService) Approve(ctx context.Context, userCode string, userID int64, scopes []string) error {
	g, err := s.Lookup(ctx, userCode)
	if err != nil {
		return err
	}
	if len(scopes) == 0 || slices.ContainsFunc(scopes, func(sc string) bool { return !slices.Contains(g.Scopes, sc) }) {
		return ErrInvalidScope
	}
	return s.decide(s.grants.Approve(ctx, g.UserCode, userID, scopes, s.now()))
}

func (s *DeviceGrantService) Deny(ctx context.Context, userCode string, userID int64) error {
	g, err := s.Lookup(ctx, userCode)
	if err != nil {
		return err
	}
	return s.decide(s.grants.Deny(ctx, g.UserCode, userID, s.now()))
}

func (s *DeviceGrantService) decide(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceGrantNotFound
	}
	return err
}

func normalizeUserCode(in string) (string, bool) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(in))
	if len(code) != userCodeLen || strings.Trim(code, userCodeAlphabet) != "" {
		return "", false
	}
	return code, true
}

func newUserCode() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(userCodeAlphabet)))
	for i := 0; i < userCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate user code: %w", err)
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate device code: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashDeviceCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// cleanDeviceName drops control and format characters (bidi overrides could reorder the
// name on the approval page), trims, and caps the length.
func cleanDeviceName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > deviceNameMax {
		s = strings.TrimSpace(string(r[:deviceNameMax]))
	}
	return s
}

func tokenName(deviceName string, now time.Time) string {
	label := defaultDeviceName
	if deviceName != "" {
		label = "cz (" + deviceName + ")"
	}
	return label + " · " + now.UTC().Format("2006-01-02")
}
