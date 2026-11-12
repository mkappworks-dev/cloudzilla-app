package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrTeamNotFound    = errors.New("team not found")
	ErrTeamOwnerOnly   = errors.New("only org owners can manage teams")
	ErrNotOrgMember    = store.ErrNotOrgMember
	ErrTeamSlugTaken   = store.ErrTeamSlugTaken
	ErrInvalidTeamName = errors.New("invalid team name")
)

const (
	maxTeamNameLen        = 64
	maxTeamDescriptionLen = 500
)

// TeamService manages the teams of an organization. A team has no effect on repository access yet.
type TeamService struct {
	teams *store.TeamStore
	orgs  *OrgService
}

func NewTeamService(teams *store.TeamStore, orgs *OrgService) *TeamService {
	return &TeamService{teams: teams, orgs: orgs}
}

// TeamSlug lowercases name and collapses every run of other characters to one hyphen.
func TeamSlug(name string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		} else {
			hyphen = true
		}
	}
	return b.String()
}

func normalizeTeam(name, description string) (cleanName, slug, cleanDesc string, err error) {
	cleanName = strings.TrimSpace(name)
	cleanDesc = strings.TrimSpace(description)
	slug = TeamSlug(cleanName)
	if slug == "" {
		return "", "", "", fmt.Errorf("%w: use letters or digits", ErrInvalidTeamName)
	}
	if utf8.RuneCountInString(cleanName) > maxTeamNameLen {
		return "", "", "", fmt.Errorf("%w: at most %d characters", ErrInvalidTeamName, maxTeamNameLen)
	}
	if utf8.RuneCountInString(cleanDesc) > maxTeamDescriptionLen {
		return "", "", "", fmt.Errorf("%w: description is at most %d characters", ErrInvalidTeamName, maxTeamDescriptionLen)
	}
	return cleanName, slug, cleanDesc, nil
}

func (s *TeamService) requireOwner(ctx context.Context, orgID, userID int64) error {
	if !s.orgs.IsOwner(ctx, orgID, userID) {
		return ErrTeamOwnerOnly
	}
	return nil
}

// requireMember hides teams from outsiders as ErrTeamNotFound.
func (s *TeamService) requireMember(ctx context.Context, orgID, userID int64) error {
	if !s.orgs.IsMember(ctx, orgID, userID) {
		return ErrTeamNotFound
	}
	return nil
}

func (s *TeamService) Create(ctx context.Context, orgID, requestingUserID int64, name, description string) (*model.Team, error) {
	if err := s.requireOwner(ctx, orgID, requestingUserID); err != nil {
		return nil, err
	}
	name, slug, description, err := normalizeTeam(name, description)
	if err != nil {
		return nil, err
	}
	t := &model.Team{OrgID: orgID, Name: name, Slug: slug, Description: description}
	if err := s.teams.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TeamService) List(ctx context.Context, orgID, viewerID int64) ([]model.Team, error) {
	if err := s.requireMember(ctx, orgID, viewerID); err != nil {
		return nil, err
	}
	return s.teams.ListByOrg(ctx, orgID)
}

func (s *TeamService) Get(ctx context.Context, orgID, viewerID int64, slug string) (*model.Team, error) {
	if err := s.requireMember(ctx, orgID, viewerID); err != nil {
		return nil, err
	}
	t, err := s.teams.GetBySlug(ctx, orgID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTeamNotFound
	}
	return t, err
}

// Update renames the team, and its slug with it: the page URL changes.
func (s *TeamService) Update(ctx context.Context, orgID, requestingUserID int64, slug, name, description string) (*model.Team, error) {
	if err := s.requireOwner(ctx, orgID, requestingUserID); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, orgID, requestingUserID, slug)
	if err != nil {
		return nil, err
	}
	name, newSlug, description, err := normalizeTeam(name, description)
	if err != nil {
		return nil, err
	}
	if err := s.teams.Update(ctx, orgID, t.ID, name, newSlug, description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	t.Name, t.Slug, t.Description = name, newSlug, description
	return t, nil
}

func (s *TeamService) Delete(ctx context.Context, orgID, requestingUserID int64, slug string) (*model.Team, error) {
	if err := s.requireOwner(ctx, orgID, requestingUserID); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, orgID, requestingUserID, slug)
	if err != nil {
		return nil, err
	}
	if err := s.teams.Delete(ctx, orgID, t.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return t, nil
}

func (s *TeamService) ListMembers(ctx context.Context, orgID, viewerID int64, slug string) ([]model.TeamMember, error) {
	t, err := s.Get(ctx, orgID, viewerID, slug)
	if err != nil {
		return nil, err
	}
	return s.teams.ListMembers(ctx, t.ID)
}

func (s *TeamService) AddMember(ctx context.Context, orgID, requestingUserID int64, slug string, targetUserID int64) (*model.Team, error) {
	if err := s.requireOwner(ctx, orgID, requestingUserID); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, orgID, requestingUserID, slug)
	if err != nil {
		return nil, err
	}
	if err := s.teams.AddMember(ctx, orgID, t.ID, targetUserID); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TeamService) RemoveMember(ctx context.Context, orgID, requestingUserID int64, slug string, targetUserID int64) (*model.Team, error) {
	if err := s.requireOwner(ctx, orgID, requestingUserID); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, orgID, requestingUserID, slug)
	if err != nil {
		return nil, err
	}
	if err := s.teams.RemoveMember(ctx, orgID, t.ID, targetUserID); err != nil {
		return nil, err
	}
	return t, nil
}
