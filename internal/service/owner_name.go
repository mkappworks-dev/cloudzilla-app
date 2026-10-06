package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var ErrInvalidOwnerName = errors.New("invalid name")

const maxOwnerNameLen = 39

var ownerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$`)

// An owner name is the first URL segment, so it can't be a top-level route.
// ghost is the user that deleted accounts' content passes to.
var reservedOwnerNames = map[string]bool{
	"activity": true, "admin": true, "api": true, "apps": true, "attention": true, "avatars": true,
	"auth": true, "authorizations": true, "explore": true, "file-row": true,
	"fragments": true, "from-template": true, "ghost": true, "gists": true, "invitations": true,
	"invite": true, "issues": true, "latest": true, "login": true, "logout": true,
	"new": true, "notifications": true, "oauth": true, "organizations": true, "orgs": true, "pulls": true,
	"read-all": true, "register": true, "repos": true, "search": true,
	"settings": true, "setup": true, "stars": true, "static": true, "topic": true,
	"unread-count": true, "verify-email": true,
}

// ValidateOwnerName checks a new user or organization name. It runs only on
// create: names stored before the rule existed keep working.
func ValidateOwnerName(name string) error {
	if !ownerNameRe.MatchString(name) {
		return fmt.Errorf("%w: use 1-39 letters, digits, - or _, starting with a letter or digit", ErrInvalidOwnerName)
	}
	if reservedOwnerNames[strings.ToLower(name)] {
		return fmt.Errorf("%w: reserved", ErrInvalidOwnerName)
	}
	return nil
}

// fitOwnerName shapes an auto-provisioned name to the owner-name rule,
// returning fallback when nothing valid is left.
func fitOwnerName(base, fallback string) string {
	name := strings.Trim(base, "_-")
	if len(name) > maxOwnerNameLen {
		name = name[:maxOwnerNameLen]
	}
	if ValidateOwnerName(name) != nil {
		return fallback
	}
	return name
}

// freeOwnerName returns base, or base with the lowest numeric suffix from 2,
// that no user or org holds in any case. The suffix replaces base's tail when
// needed to stay within the length limit.
func freeOwnerName(ctx context.Context, users *store.UserStore, base string) string {
	candidate := base
	for i := 2; ; i++ {
		// On a lookup error the insert's own guard still refuses a taken name.
		if taken, err := users.OwnerNameTaken(ctx, candidate); err != nil || !taken {
			return candidate
		}
		suffix := strconv.Itoa(i)
		candidate = base[:min(len(base), maxOwnerNameLen-len(suffix))] + suffix
	}
}
