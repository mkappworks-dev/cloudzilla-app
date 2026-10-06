package handler

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const (
	ownerNameRule          = `can use letters, numbers, - and _, must start with a letter or number, be at most 39 characters, and can't be a reserved name like "admin".`
	invalidUsernameMessage = "Usernames " + ownerNameRule
	invalidOrgNameMessage  = "Organization names " + ownerNameRule
	invalidRepoNameMessage = "Repository names can use letters, numbers, ., - and _, must start with a letter or number, be at most 100 characters, and can't end in .wiki."
	unsafeRepoPathMessage  = "This repository can't be created: the owner or template name isn't allowed in repository paths. Ask an administrator to rename it."
)

// createFailedMessage returns the page text for a failed create. Errors the user
// can act on get their own message; anything else is logged and replaced,
// because store and driver errors carry constraint names and SQLSTATEs.
const pullIntoMirrorMessage = "Pull mirrors are read-only; open the pull request upstream."

func createFailedMessage(err error, what string, logAttrs ...any) string {
	switch {
	case errors.Is(err, service.ErrTitleTooLong):
		return fmt.Sprintf("Title is too long (maximum %d characters)", service.MaxTitleLen)
	case errors.Is(err, service.ErrPrivateIssueForbidden):
		return "Only collaborators with write access can create private issues"
	case errors.Is(err, service.ErrUnknownDiscussionCategory):
		return "Pick a category for your discussion"
	case errors.Is(err, service.ErrPullIntoMirror):
		return pullIntoMirrorMessage
	}
	slog.Error("new "+what+": create failed", append(logAttrs, "error", err)...)
	return "Could not create the " + what + ". Please try again."
}

func passwordLengthMessage(password string) string {
	if len(password) > service.MaxPasswordBytes {
		return fmt.Sprintf("Password is too long (maximum %d bytes)", service.MaxPasswordBytes)
	}
	return ""
}
