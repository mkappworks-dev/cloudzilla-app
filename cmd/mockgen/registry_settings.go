package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func settingsPages() []page {
	return []page{
		{
			Name: "account_settings", Route: "/settings", Templ: "settings.templ", Title: "Settings",
			Variants: []variant{
				vr("", "profile, keys, tokens and security", Writer, func(b view.BasePage) templ.Component {
					return pages.Settings(settingsData(b))
				}),
				vr("fresh", "new account: no keys, tokens or apps, two-factor off, email unverified", Writer, func(b view.BasePage) templ.Component {
					d := settingsData(b)
					d.User.EmailVerifiedAt, d.TOTPEnabled = nil, false
					d.SSHKeys, d.Tokens, d.SavedReplies, d.OAuthApps = nil, nil, nil, nil
					d.GoogleConfigured = false
					return pages.Settings(d)
				}),
				vr("error", "profile save failed", Writer, func(b view.BasePage) templ.Component {
					d := settingsData(b)
					d.ProfileError = "Name is too long."
					d.PasswordError = "The current password is wrong."
					return pages.Settings(d)
				}),
				vr("unverified", "email not yet verified, the instance can send email", Writer, func(b view.BasePage) templ.Component {
					d := settingsData(b)
					d.User.EmailVerifiedAt = nil
					d.EmailVerificationAvailable = true
					return pages.Settings(d)
				}),
				vr("totp-setup", "two-factor enrolment in progress", Writer, func(b view.BasePage) templ.Component {
					d := settingsData(b)
					d.TOTPEnabled = false
					d.TOTPPendingSecret = "JBSWY3DPEHPK3PXP"
					return pages.Settings(d)
				}),
				vr("new-token", "a token was just created", Writer, func(b view.BasePage) templ.Component {
					d := settingsData(b)
					d.NewToken = "czp_0123456789abcdef0123456789abcdef"
					return pages.Settings(d)
				}),
			},
		},
		{
			Name: "org_settings", Route: "/orgs/{org}/settings", Templ: "org_settings.templ", Title: "Organization settings",
			Variants: []variant{
				vr("", "members, defaults and danger zone", Writer, func(b view.BasePage) templ.Component {
					return pages.OrgSettings(orgSettingsData(b))
				}),
				vr("no-repos", "organization with no repositories", Writer, func(b view.BasePage) templ.Component {
					d := orgSettingsData(b)
					d.RepoCount = 0
					d.AuditEntries = nil
					return pages.OrgSettings(d)
				}),
			},
		},
		{
			Name: "explore", Route: "/explore", Templ: "explore.templ", Title: "Explore",
			Variants: []variant{
				vr("", "trending repositories", Writer, func(b view.BasePage) templ.Component {
					return pages.Explore(exploreData(b, "trending", true))
				}),
				vr("anon", "signed out", Anon, func(b view.BasePage) templ.Component {
					return pages.Explore(exploreData(b, "trending", true))
				}),
				vr("empty", "nothing to show", Writer, func(b view.BasePage) templ.Component {
					return pages.Explore(exploreData(b, "trending", false))
				}),
			},
		},
		{
			Name: "setup", Route: "/setup", Templ: "setup.templ", Title: "Set up Cloudzilla",
			Variants: []variant{
				vr("", "first-run admin account", Anon, func(b view.BasePage) templ.Component {
					return pages.Setup(view.SetupData{})
				}),
				vr("error", "a required field is blank", Anon, func(b view.BasePage) templ.Component {
					return pages.Setup(view.SetupData{Error: "All fields are required"})
				}),
			},
		},
		{
			Name: "oauth_authorize", Route: "/oauth/authorize", Templ: "oauth_authorize.templ", Title: "Authorize application",
			Variants: []variant{
				vr("", "consent screen", Writer, func(b view.BasePage) templ.Component {
					return pages.OAuthAuthorize(oauthData(b, ""))
				}),
				vr("error", "redirect URI rejected", Writer, func(b view.BasePage) templ.Component {
					return pages.OAuthAuthorize(oauthData(b, "That redirect URI is not registered for this application."))
				}),
			},
		},
		{
			Name: "device_login", Route: "/login/device", Templ: "device_login.templ", Title: "Device login",
			Variants: []variant{
				vr("", "enter the code", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceEntry(view.DeviceEntryData{BasePage: b})
				}),
				vr("error", "unknown code", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceEntry(view.DeviceEntryData{BasePage: b, Error: "That code was not found or has expired."})
				}),
				vr("confirm", "approve the request", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceConfirm(deviceConfirmData(b, ""))
				}),
				vr("confirm-error", "wrong password", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceConfirm(deviceConfirmData(b, "Wrong password."))
				}),
				vr("approved", "after approving", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceDone(view.DeviceDoneData{BasePage: b, Approved: true})
				}),
				vr("denied", "after denying", Writer, func(b view.BasePage) templ.Component {
					return pages.DeviceDone(view.DeviceDoneData{BasePage: b})
				}),
			},
		},
	}
}

func settingsData(b view.BasePage) view.SettingsData {
	u := fixtureUserRow()
	u.EmailVerifiedAt = timePtr(ago(days(250)))
	u.TOTPEnabled = true
	return view.SettingsData{
		BasePage: onAccountPage(b, "settings"), User: u, TOTPEnabled: true, HasPassword: true, NoreplyEmail: "1+mkappworks@users.noreply.cloudzilla.example.test",
		Confirm:      passwordFactor,
		SSHKeys:      []model.SSHKey{{ID: 1, UserID: 1, Title: "Work laptop", Fingerprint: "SHA256:3b5Lq0oVwY8w2n4mK1xYb9T1l1r1uZq6jY6tY2uJk0A", CreatedAt: ago(days(200))}},
		Tokens:       []model.AccessToken{{ID: 1, UserID: 1, Name: "ci", LastEight: "a1b2c3d4", Scopes: []string{"repo"}, CreatedAt: ago(days(60)), LastUsedAt: timePtr(ago(days(3)))}},
		SavedReplies: []model.SavedReply{{ID: 1, UserID: 1, Title: "Thanks", Body: "Thanks for the report!", CreatedAt: ago(days(90)), UpdatedAt: ago(days(90))}},
		OAuthApps: []model.OAuthApp{{ID: 1, OwnerID: 1, Name: "Deploy bot", ClientID: "cz_client_0123", RedirectURIs: []string{"https://deploy.example.test/callback"},
			HomepageURL: "https://deploy.example.test", CreatedAt: ago(days(40))}},
		GoogleConfigured: true,
	}
}

func orgSettingsData(b view.BasePage) view.OrgSettingsData {
	return view.OrgSettingsData{
		BasePage: b, Org: fixtureOrg(), Members: fixtureMembers(), MemberCount: 4, RepoCount: 4, Confirm: passwordFactor,
		AuditEntries: []model.AuditEntry{
			{ID: 2, ActorName: fixtureUser, Action: model.AuditActionOrgProfileUpdate, TargetType: model.AuditTargetOrg, TargetName: "paper-planes", CreatedAt: ago(days(20))},
			{ID: 1, ActorName: "ada-silva", Action: model.AuditActionRepoCreate, TargetType: model.AuditTargetRepo, TargetName: "paper-planes/ledger", CreatedAt: ago(days(30))},
		},
	}
}

func exploreData(b view.BasePage, tab string, withRepos bool) view.ExploreData {
	d := view.ExploreData{BasePage: b, Tab: tab, Period: "weekly"}
	if withRepos {
		for i, r := range fixtureRepos()[:2] {
			d.Repos = append(d.Repos, model.RepositoryWithStats{Repository: r, StarCount: 128 / (i + 1)})
		}
	}
	return d
}

func oauthData(b view.BasePage, errMsg string) view.OAuthAuthorizeData {
	return view.OAuthAuthorizeData{
		BasePage: b, Confirm: passwordFactor, Error: errMsg, Scopes: []string{"repo", "read:user"}, RedirectURI: "https://deploy.example.test/callback", State: "xyz",
		App: model.OAuthApp{ID: 1, Name: "Deploy bot", ClientID: "cz_client_0123", Description: "Ships tagged releases to the staging fleet.", HomepageURL: "https://deploy.example.test"},
	}
}

func deviceConfirmData(b view.BasePage, errMsg string) view.DeviceConfirmData {
	return view.DeviceConfirmData{
		BasePage: b, Username: fixtureUser, UserCode: "WDJB-MJHT", DeviceName: "cz on mk-laptop", RequesterIP: "203.0.113.7", ViewerIP: "203.0.113.7",
		RequestedAt: "just now", Scopes: []string{"repo", "read:user"}, Selected: []string{"repo", "read:user"}, Confirm: passwordFactor, Error: errMsg,
	}
}
