package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/db"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func reset2FACmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-2fa <username>",
		Short: "Turn off a user's two-factor authentication",
		Long: "Turns off two-factor authentication for a user who has lost their authenticator and backup codes, " +
			"clearing the TOTP secret and backup codes, as the admin reset-2fa action does. The user is mailed a security notice " +
			"when SMTP is configured. Sessions stay signed in.",
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

			return reset2FA(cmd.Context(), store.New(database), cfg, args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func reset2FA(ctx context.Context, stores *store.Stores, cfg *config.Config, username string, stdout, stderr io.Writer) error {
	audit := service.NewAuditService(stores.AuditLog)
	admin := service.NewAdminUserService(stores.User, nil, audit).WithSecurityNotices(service.NewEmailService(cfg.SMTP))
	u, changed, err := admin.ResetTOTPOffline(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no user named %q", username)
	}
	if err != nil {
		return err
	}
	if !changed {
		_, err := fmt.Fprintf(stdout, "@%s doesn't have two-factor authentication turned on; nothing to reset.\n", u.Username)
		return err
	}
	auditErr := audit.RecordOffline(ctx, "cloudzilla-cli", model.AuditActionAdminUser2FAReset,
		model.AuditTargetUser, u.ID, u.Username, nil)
	// 2FA is already off, so the owner hears about it even when the audit write failed.
	if err := admin.SendTOTPResetNotice(ctx, u.ID); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: couldn't mail @%s the security notice: %v\n", u.Username, err)
	}
	if auditErr != nil {
		return fmt.Errorf("two-factor authentication for @%s is off, but the audit entry failed: %w", u.Username, auditErr)
	}
	if _, err := fmt.Fprintf(stdout, "Turned off two-factor authentication for @%s.\n", u.Username); err != nil {
		return err
	}
	if u.Suspended() {
		_, err := fmt.Fprintf(stdout, "@%s is still suspended, so they can't sign in until an admin unsuspends them.\n", u.Username)
		return err
	}
	return nil
}
