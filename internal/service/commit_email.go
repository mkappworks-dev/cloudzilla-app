package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const noreplyDomainPrefix = "users.noreply."

func noreplyHostFromBaseURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return "localhost"
	}
	return u.Hostname()
}

func noreplyEmail(host string, u *model.User) string {
	return fmt.Sprintf("%d+%s@%s%s", u.ID, u.Username, noreplyDomainPrefix, host)
}

// Any host after the prefix is accepted: base_url usually changes after install
// (the default is localhost), and commits already made keep the old host.
func parseNoreplyEmail(email string) (id int64, username string, ok bool) {
	local, domain, found := strings.Cut(email, "@")
	if !found || len(domain) <= len(noreplyDomainPrefix) ||
		!strings.EqualFold(domain[:len(noreplyDomainPrefix)], noreplyDomainPrefix) {
		return 0, "", false
	}
	idStr, username, found := strings.Cut(local, "+")
	if !found || username == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return id, username, true
}

// Legacy username@localhost authors are deliberately not resolved: unconfigured
// git clients emit addresses like root@localhost, which would credit whoever owns
// that username.
func userByAuthorEmail(ctx context.Context, users *store.UserStore, email string) (*model.User, error) {
	if id, username, ok := parseNoreplyEmail(email); ok {
		u, err := users.GetByID(ctx, id)
		if err == nil && u.Username == username {
			return u, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return users.GetByEmail(ctx, email)
}
