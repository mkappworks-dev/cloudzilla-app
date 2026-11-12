package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func vr(name, note string, viewer Viewer, render func(view.BasePage) templ.Component) variant {
	return variant{Name: name, Note: note, Viewer: viewer, Render: render}
}

func authPages() []page {
	return []page{
		{
			Name: "register", Route: "/register", Templ: "register.templ", Title: "Create account",
			Variants: []variant{
				vr("", "username, email and password", Anon, func(b view.BasePage) templ.Component {
					return pages.Register(view.RegisterData{BasePage: b})
				}),
				vr("error", "username already taken", Anon, func(b view.BasePage) templ.Component {
					return pages.Register(view.RegisterData{BasePage: b, Username: "ada-silva", Email: "ada@example.test", Error: "That username is already taken."})
				}),
			},
		},
		{
			Name: "register_email", Route: "/register", Templ: "signup.templ", Title: "Create account",
			Variants: []variant{
				vr("", "email-first signup, shown when the instance can send email", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterEmail(view.RegisterEmailData{BasePage: b})
				}),
				vr("error", "address is not an email", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterEmail(view.RegisterEmailData{BasePage: b, Email: "ada@", Error: "Enter a valid email address"})
				}),
				vr("check-inbox", "shown after every submit", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterCheckInbox(view.RegisterCheckInboxData{BasePage: b})
				}),
			},
		},
		{
			Name: "signup_complete", Route: "/register/complete/{token}", Templ: "signup.templ", Title: "Create account",
			Variants: []variant{
				vr("", "choose a username and password from the emailed link", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterComplete(view.RegisterCompleteData{BasePage: b, Signup: &model.SignupToken{ID: 1, Email: "ada@example.test", ExpiresAt: fixedNow.Add(24 * hour)}})
				}),
				vr("error", "username already taken", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterComplete(view.RegisterCompleteData{BasePage: b, Signup: &model.SignupToken{ID: 1, Email: "ada@example.test", ExpiresAt: fixedNow.Add(24 * hour)}, Username: "ada-silva", Error: "That username is already taken."})
				}),
				vr("invalid", "link used or expired", Anon, func(b view.BasePage) templ.Component {
					return pages.RegisterComplete(view.RegisterCompleteData{BasePage: b})
				}),
			},
		},
		{
			Name: "forgot_password", Route: "/auth/password/forgot", Templ: "password_reset.templ", Title: "Reset password",
			Variants: []variant{
				vr("", "email form", Anon, func(b view.BasePage) templ.Component {
					return pages.ForgotPassword(view.ForgotPasswordData{BasePage: b, Available: true})
				}),
				vr("sent", "the page every valid address gets", Anon, func(b view.BasePage) templ.Component {
					return pages.ForgotPassword(view.ForgotPasswordData{BasePage: b, Available: true, Sent: true, Email: "ada@example.test"})
				}),
				vr("error", "address is not an email", Anon, func(b view.BasePage) templ.Component {
					return pages.ForgotPassword(view.ForgotPasswordData{BasePage: b, Available: true, Email: "ada@", Error: "Enter a valid email address."})
				}),
				vr("unavailable", "the instance cannot send email", Anon, func(b view.BasePage) templ.Component {
					return pages.ForgotPassword(view.ForgotPasswordData{BasePage: b})
				}),
			},
		},
		{
			Name: "reset_password", Route: "/auth/password/reset/{token}", Templ: "password_reset.templ", Title: "Choose a new password",
			Variants: []variant{
				vr("", "pending link", Anon, func(b view.BasePage) templ.Component {
					return pages.ResetPassword(view.ResetPasswordData{BasePage: b, Link: pendingReset(false)})
				}),
				vr("totp", "pending link on an account with two-factor", Anon, func(b view.BasePage) templ.Component {
					return pages.ResetPassword(view.ResetPasswordData{BasePage: b, Link: pendingReset(true)})
				}),
				vr("error", "passwords differ", Anon, func(b view.BasePage) templ.Component {
					return pages.ResetPassword(view.ResetPasswordData{BasePage: b, Link: pendingReset(false), Error: "The passwords do not match."})
				}),
				vr("expired", "link older than its lifetime", Anon, func(b view.BasePage) templ.Component {
					return pages.ResetPassword(view.ResetPasswordData{BasePage: b, Link: model.PasswordResetLink{State: model.PasswordResetExpired}})
				}),
				vr("invalid", "link used, replaced or revoked", Anon, func(b view.BasePage) templ.Component {
					return pages.ResetPassword(view.ResetPasswordData{BasePage: b, Link: model.PasswordResetLink{State: model.PasswordResetInvalid}})
				}),
			},
		},
		{
			Name: "verify_email", Route: "/verify-email", Templ: "verify_email.templ", Title: "Verify email",
			Variants: []variant{
				vr("", "pending link, signed in", Writer, func(b view.BasePage) templ.Component {
					return pages.VerifyEmail(view.VerifyEmailData{BasePage: b, Token: "token", Link: model.EmailVerificationLink{State: model.EmailVerificationPending, Username: fixtureUser, Email: "mk@example.test"}})
				}),
				vr("verified", "after confirming", Writer, func(b view.BasePage) templ.Component {
					return pages.VerifyEmail(view.VerifyEmailData{BasePage: b, Link: model.EmailVerificationLink{State: model.EmailVerificationVerified}})
				}),
				vr("expired", "link older than 24 hours", Writer, func(b view.BasePage) templ.Component {
					return pages.VerifyEmail(view.VerifyEmailData{BasePage: b, Link: model.EmailVerificationLink{State: model.EmailVerificationExpired}})
				}),
				vr("invalid", "link used, replaced or for an address the account dropped, signed out", Anon, func(b view.BasePage) templ.Component {
					return pages.VerifyEmail(view.VerifyEmailData{BasePage: b, Link: model.EmailVerificationLink{State: model.EmailVerificationInvalid}})
				}),
			},
		},
		{
			Name: "totp_verify", Route: "/auth/2fa", Templ: "totp_verify.templ", Title: "Two-factor authentication",
			Variants: []variant{
				vr("", "authenticator code", Anon, func(b view.BasePage) templ.Component {
					return pages.TOTPVerify(view.TOTPVerifyPageData{BasePage: b})
				}),
				vr("error", "wrong code", Anon, func(b view.BasePage) templ.Component {
					return pages.TOTPVerify(view.TOTPVerifyPageData{BasePage: b, Error: "Invalid code. Try again."})
				}),
			},
		},
		{
			Name: "invite", Route: "/invite/{token}", Templ: "invite.templ", Title: "Accept invitation",
			Variants: []variant{
				vr("", "valid invitation", Anon, func(b view.BasePage) templ.Component {
					return pages.Invite(view.InviteData{BasePage: b, Invitation: &model.Invitation{ID: 1, Email: "ada@example.test", ExpiresAt: fixedNow.Add(7 * 24 * hour), CreatedAt: ago(hour)}})
				}),
				vr("error", "username already taken", Anon, func(b view.BasePage) templ.Component {
					return pages.Invite(view.InviteData{BasePage: b, Invitation: &model.Invitation{ID: 1, Email: "ada@example.test", ExpiresAt: fixedNow.Add(7 * 24 * hour), CreatedAt: ago(hour)}, Error: "That username is already taken."})
				}),
				vr("invalid", "link used or expired", Anon, func(b view.BasePage) templ.Component {
					return pages.Invite(view.InviteData{BasePage: b})
				}),
			},
		},
	}
}

func pendingReset(totp bool) model.PasswordResetLink {
	return model.PasswordResetLink{
		State: model.PasswordResetPending, UserID: 1, Username: "ada-silva", Email: "ada@example.test",
		IssuedBy: model.PasswordResetByEmail, TOTPEnabled: totp,
	}
}
