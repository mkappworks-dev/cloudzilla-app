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

// EmailOwners maps each lower-cased address in emails to the account that owns it as a verified address, the noreply
// address included. An address nobody has verified is left out.
func (s *UserService) EmailOwners(ctx context.Context, emails []string) (map[string]int64, error) {
	var stored []string
	noreply := map[string]int64{}
	var noreplyIDs []int64
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if id, _, ok := parseNoreplyEmail(e); ok {
			noreply[e] = id
			noreplyIDs = append(noreplyIDs, id)
			continue
		}
		stored = append(stored, e)
	}
	out, err := s.store.VerifiedEmailOwners(ctx, stored)
	if err != nil {
		return nil, err
	}
	if len(noreplyIDs) == 0 {
		return out, nil
	}
	accounts, err := s.store.SignerIdentities(ctx, noreplyIDs)
	if err != nil {
		return nil, err
	}
	for e, id := range noreply {
		_, name, _ := parseNoreplyEmail(e)
		if a := accounts[id]; a != nil && strings.EqualFold(a.Username, name) {
			out[e] = id
		}
	}
	return out, nil
}
