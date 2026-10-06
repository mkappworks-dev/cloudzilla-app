package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func passwordResetLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "password-reset-link <username>",
		Short: "Print a 24-hour password reset link for a user",
		Long: "Prints a single-use link that lets the user choose a new password, for passing on by hand. " +
			"It needs no SMTP and replaces any link the user already has. Accounts that sign in with Google, LDAP or SAML have no password and get no link.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}
			database, err := db.Connect(cfg.Database)
			if err != nil {
				return err
			}
			defer func() { _ = database.Close() }()

			link, err := issuePasswordResetLink(cmd.Context(), store.New(database), cfg, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), link)
			return err
		},
	}
}

func issuePasswordResetLink(ctx context.Context, stores *store.Stores, cfg *config.Config, username string) (string, error) {
	u, err := stores.User.GetByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no user named %q", username)
	}
	if err != nil {
		return "", err
	}
	resets := service.NewPasswordResetService(stores.PasswordReset, stores.User, nil, service.NewEmailService(cfg.SMTP), cfg.Server.BaseURL)
	link, err := resets.IssueLink(ctx, u.ID, model.PasswordResetByCLI)
	if errors.Is(err, service.ErrPasswordResetNoPassword) {
		return "", fmt.Errorf("@%s has no password to reset: it signs in with Google, LDAP or SAML", u.Username)
	}
	if err != nil {
		return "", err
	}
	// A link that isn't in the audit log is never shown.
	if err := service.NewAuditService(stores.AuditLog).RecordOffline(ctx, "cloudzilla-cli", model.AuditActionPasswordResetLink,
		model.AuditTargetUser, u.ID, u.Username, map[string]any{"issued_by": model.PasswordResetByCLI}); err != nil {
		return "", err
	}
	return link, nil
}
