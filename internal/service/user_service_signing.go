package service

import (
	"context"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// SigningIdentity is an account as commit-signature verification sees it.
type SigningIdentity struct {
	Username string
	// VerifiedEmails are lower-cased: the primary address if verified, the verified additional ones, and the noreply
	// address.
	VerifiedEmails []string
}

// SigningIdentities returns the identity of each account in userIDs that exists.
func (s *UserService) SigningIdentities(ctx context.Context, userIDs []int64) (map[int64]SigningIdentity, error) {
	rows, err := s.store.SignerIdentities(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]SigningIdentity, len(rows))
	for id, r := range rows {
		emails := []string{strings.ToLower(noreplyEmail(s.noreplyHost, &model.User{ID: id, Username: r.Username}))}
		if r.Email != "" {
			emails = append(emails, strings.ToLower(r.Email))
		}
		for _, e := range r.ExtraEmails {
			emails = append(emails, strings.ToLower(e))
		}
		out[id] = SigningIdentity{Username: r.Username, VerifiedEmails: emails}
	}
	return out, nil
}
