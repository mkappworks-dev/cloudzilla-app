package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrOAuthLinkInvalid     = errors.New("the connect request is unknown, already used, or expired")
	ErrOAuthLinkWrongUser   = errors.New("the connect request was started by a different account")
	ErrOAuthAlreadyLinked   = errors.New("this account is already connected to a Google account")
	ErrOAuthLinkedElsewhere = errors.New("that Google account is connected to a different account")
	ErrOAuthNotLinked       = errors.New("this account is not connected to that provider")
)

const oauthLinkTTL = 5 * time.Minute

var oauthProviderNames = map[string]string{"google": "Google"}

// OAuthLinkService connects a provider sign-in to an existing account, and removes it.
// Both need the account's password and TOTP code, so a stolen session cannot
// plant a sign-in of its own that outlives the session.
type OAuthLinkService struct {
	users  *store.UserStore
	states *store.OAuthStateStore
	reauth *ReauthService
	email  *EmailService
}

func NewOAuthLinkService(users *store.UserStore, states *store.OAuthStateStore, totp *TOTPService, email *EmailService) *OAuthLinkService {
	return &OAuthLinkService{users: users, states: states, reauth: NewReauthService(users, totp), email: email}
}

func (s *OAuthLinkService) reauthenticate(ctx context.Context, userID int64, password, code string) (*model.User, error) {
	return s.reauth.ConfirmWithPassword(ctx, userID, Confirmation{Password: password, Code: code})
}

// BeginLink re-authenticates userID and returns a single-use state for the
// provider's authorization URL, with its expiry. A newer state replaces an older one.
func (s *OAuthLinkService) BeginLink(ctx context.Context, userID int64, password, code string) (string, time.Time, error) {
	u, err := s.reauthenticate(ctx, userID, password, code)
	if err != nil {
		return "", time.Time{}, err
	}
	if u.OAuthProvider != "" {
		return "", time.Time{}, ErrOAuthAlreadyLinked
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate oauth state: %w", err)
	}
	state := hex.EncodeToString(raw)
	expiresAt, err := s.states.Put(ctx, userID, model.OAuthStatePurposeLink, sha256HexOf(state), oauthLinkTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return state, expiresAt, nil
}

// LinkGrant is what ConsumeLinkState hands Link. Only this package can fill one in,
// so no caller can link an account without a re-authenticated, spent state.
type LinkGrant struct{ userID int64 }

// ConsumeLinkState redeems state for sessionUserID, the account signed in at the
// callback. Every attempt spends the state, so a refused one cannot be retried.
func (s *OAuthLinkService) ConsumeLinkState(ctx context.Context, state string, sessionUserID int64) (LinkGrant, error) {
	userID, err := s.states.Take(ctx, sha256HexOf(state), model.OAuthStatePurposeLink)
	if errors.Is(err, sql.ErrNoRows) {
		return LinkGrant{}, ErrOAuthLinkInvalid
	}
	if err != nil {
		return LinkGrant{}, err
	}
	if userID != sessionUserID {
		return LinkGrant{}, ErrOAuthLinkWrongUser
	}
	return LinkGrant{userID: userID}, nil
}

// Link connects id to the account grant names. It never creates or signs in an
// account, and reports false when that account already had id.
func (s *OAuthLinkService) Link(ctx context.Context, grant LinkGrant, id OAuthIdentity) (bool, error) {
	if grant.userID == 0 {
		return false, ErrOAuthLinkInvalid
	}
	userID := grant.userID
	// The audit entry and the notice name this address, so Google must vouch for it.
	if !id.EmailVerified {
		return false, ErrOAuthEmailUnverified
	}
	holder, err := s.users.GetByOAuthID(ctx, id.Provider, id.ID)
	switch {
	case err == nil && holder.ID == userID:
		return false, nil
	case err == nil:
		return false, ErrOAuthLinkedElsewhere
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	linked, err := s.users.LinkOAuth(ctx, userID, id.Provider, id.ID)
	if errors.Is(err, store.ErrOAuthIdentityTaken) {
		return false, ErrOAuthLinkedElsewhere
	}
	if err != nil {
		return false, err
	}
	if !linked {
		return false, ErrOAuthAlreadyLinked
	}
	s.notify(userID, id.Provider, id.Email, true)
	return true, nil
}

// LinkByVerifiedEmail makes the link AuthenticateOAuth offered. The UPDATE
// re-checks the account, since its email may have changed since the match.
func (s *OAuthLinkService) LinkByVerifiedEmail(ctx context.Context, l OAuthLink) (*model.User, error) {
	u, err := s.users.LinkOAuthByVerifiedEmail(ctx, l.UserID, l.Email, l.Provider, l.ID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrOAuthIdentityTaken) {
		return nil, ErrOAuthAccountExists
	}
	if err != nil {
		return nil, err
	}
	s.notify(u.ID, l.Provider, l.Email, true)
	return u, nil
}

// Unlink removes userID's provider sign-in after re-authentication and returns the
// provider ID it held. An account without a password is refused: the provider is its only way in.
func (s *OAuthLinkService) Unlink(ctx context.Context, userID int64, provider, password, code string) (string, error) {
	u, err := s.reauthenticate(ctx, userID, password, code)
	if err != nil {
		return "", err
	}
	unlinked, err := s.users.UnlinkOAuth(ctx, userID, provider)
	if err != nil {
		return "", err
	}
	if !unlinked {
		return "", ErrOAuthNotLinked
	}
	s.notify(userID, provider, "", false)
	return u.OAuthID, nil
}

func (s *OAuthLinkService) notify(userID int64, provider, providerEmail string, connected bool) {
	concurrency.Go("oauth_link.notice", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := s.users.GetByID(ctx, userID)
		if err != nil {
			slog.Error("oauth link notice: load user", "user_id", userID, "error", err)
			return
		}
		subject, body := oauthLinkNotice(u.Username, oauthProviderNames[provider], providerEmail, connected)
		if err := s.email.SendSecurityNotice(u, subject, body); err != nil {
			slog.Error("oauth link notice: send", "user_id", userID, "error", err)
		}
	})
}

func oauthLinkNotice(username, providerName, providerEmail string, connected bool) (subject, body string) {
	user := html.EscapeString(username)
	name := html.EscapeString(providerName)
	if connected {
		return providerName + " sign-in was connected to your account",
			fmt.Sprintf("<p>The %s account <strong>%s</strong> can now sign in to <strong>@%s</strong>.</p>"+
				"<p>If you didn't connect it, someone else can sign in as you. Disconnect it under Account settings → Security and tell your administrator.</p>",
				name, html.EscapeString(providerEmail), user)
	}
	return providerName + " sign-in was removed from your account",
		fmt.Sprintf("<p>%s sign-in was removed from <strong>@%s</strong>. Sign in with your password from now on.</p>"+
			"<p>If you didn't remove it, someone else can sign in as you. Tell your administrator.</p>",
			name, user)
}
