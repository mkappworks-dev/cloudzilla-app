package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrAdminTokenNeedsTargets = errors.New("a repo:admin token must name the repositories or organizations it may administer")
	ErrTokenTarget            = errors.New("not a repository or organization you administer")
)

const maxTokenTargets = 50

// adminTargets resolves the repositories and organizations a repo:admin token
// is limited to, which its creator must administer.
type adminTargets struct {
	repos *RepoService
	orgs  *OrgService
}

// canonical returns target as the token stores it: "owner/repo" for a
// repository userID can manage, or "org" for an organization they own. A
// target that doesn't exist and one they can't administer get the same error,
// so a private repository's name isn't confirmed.
func (a adminTargets) canonical(ctx context.Context, userID int64, target string) (string, error) {
	target = strings.TrimSpace(target)
	if owner, name, ok := strings.Cut(target, "/"); ok {
		repo, err := a.repos.Get(ctx, owner, name)
		if err != nil || !a.repos.CanManage(ctx, repo, userID) {
			return "", fmt.Errorf("%w: %s", ErrTokenTarget, target)
		}
		return repo.OwnerName + "/" + repo.Name, nil
	}
	org, err := a.orgs.Get(ctx, target)
	if err != nil || !a.orgs.IsOwner(ctx, org.ID, userID) {
		return "", fmt.Errorf("%w: %s", ErrTokenTarget, target)
	}
	return org.Name, nil
}
