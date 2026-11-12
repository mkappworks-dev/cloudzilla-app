package main

import (
	"time"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var passwordFactor = components.ConfirmFactors{Password: true}

func adminPages() []page {
	return []page{
		{
			Name: "admin_settings", Route: "/admin/settings", Templ: "admin_settings.templ", Title: "Admin settings",
			Variants: []variant{
				vr("", "site settings and invitations", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminSettings(adminSettingsData(b))
				}),
				{Name: "admin-mode", Note: "instance admin acting with elevated rights", Viewer: Admin, AdminMode: true,
					Render: func(b view.BasePage) templ.Component { return pages.AdminSettings(adminSettingsData(b)) }},
				vr("empty", "no invitations sent", Admin, func(b view.BasePage) templ.Component {
					d := adminSettingsData(b)
					d.Invitations = nil
					return pages.AdminSettings(d)
				}),
			},
		},
		{
			Name: "admin_users", Route: "/admin/users", Templ: "admin_users.templ", Title: "Users",
			Variants: []variant{
				vr("", "first page of users", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminUsers(adminUsersData(b, adminUserRows()))
				}),
				vr("empty", "filter matches nobody", Admin, func(b view.BasePage) templ.Component {
					d := adminUsersData(b, nil)
					d.Filter = model.AdminUserFilter{Query: "zzz"}
					return pages.AdminUsers(d)
				}),
			},
		},
		{
			Name: "admin_user", Route: "/admin/users/{username}", Templ: "admin_users.templ", Title: "User",
			Variants: []variant{
				vr("", "another active user", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminUser(adminUserData(b, adminUserRows()[2], false))
				}),
				vr("sole-owner", "user who is the only owner of an organization", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminUser(adminUserData(b, adminUserRows()[1], false))
				}),
				vr("self", "the admin's own account offers no actions", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminUser(adminUserData(b, adminUserRows()[0], true))
				}),
				vr("suspended", "suspended user", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminUser(adminUserData(b, adminUserRows()[3], false))
				}),
			},
		},
		{
			Name: "audit_log", Route: "/admin/audit-log", Templ: "audit_log.templ", Title: "Audit log",
			Variants: []variant{
				vr("", "recent entries", Admin, func(b view.BasePage) templ.Component {
					return pages.AuditLog(view.AuditLogData{BasePage: b, Entries: fixtureAudit(), TotalCount: 3, Page: 1, PerPage: 50})
				}),
				vr("empty", "no entries match", Admin, func(b view.BasePage) templ.Component {
					return pages.AuditLog(view.AuditLogData{BasePage: b, Page: 1, PerPage: 50})
				}),
			},
		},
		{
			Name: "sso_settings", Route: "/admin/sso", Templ: "sso_settings.templ", Title: "Single sign-on",
			Variants: []variant{
				vr("", "nothing configured", Admin, func(b view.BasePage) templ.Component {
					return pages.SSOSettings(view.SSOSettingsData{BasePage: b, Confirm: passwordFactor})
				}),
				vr("configured", "LDAP on, SAML saved but off", Admin, func(b view.BasePage) templ.Component {
					return pages.SSOSettings(view.SSOSettingsData{BasePage: b, Confirm: passwordFactor, LDAPConfig: ldapConfig(true), SAMLConfig: samlConfig(false)})
				}),
				vr("error", "save failed", Admin, func(b view.BasePage) templ.Component {
					return pages.SSOSettings(view.SSOSettingsData{BasePage: b, Confirm: passwordFactor, LDAPConfig: ldapConfig(false), Error: "Could not reach the LDAP server."})
				}),
			},
		},
		{
			Name: "admin_mode", Route: "/admin/mode", Templ: "admin_mode.templ", Title: "Admin mode",
			Variants: []variant{
				vr("", "not in admin mode", Admin, func(b view.BasePage) templ.Component {
					return pages.AdminMode(view.AdminModeData{BasePage: b, Confirm: passwordFactor, Next: "/admin/users"})
				}),
				{Name: "active", Note: "in admin mode, with the banner", Viewer: Admin, AdminMode: true, Render: func(b view.BasePage) templ.Component {
					return pages.AdminMode(view.AdminModeData{BasePage: b, Confirm: passwordFactor, Next: "/admin/users"})
				}},
			},
		},
		{
			Name: "forbidden", Route: "any 403", Templ: "forbidden.templ", Title: "Forbidden",
			Variants: []variant{
				vr("", "signed in", Writer, func(b view.BasePage) templ.Component { return pages.Forbidden(b) }),
				vr("anon", "signed out", Anon, func(b view.BasePage) templ.Component { return pages.Forbidden(b) }),
				vr("admin-hint", "superadmin who could open the page in admin mode", Admin, func(b view.BasePage) templ.Component {
					return pages.ForbiddenAdminMode(b, "/admin/users")
				}),
			},
		},
	}
}

func adminSettingsData(b view.BasePage) view.AdminSettingsData {
	return view.AdminSettingsData{
		BasePage: b, Confirm: passwordFactor,
		Settings: []model.SiteSetting{{Key: "allow_registration", Value: "true"}},
		Invitations: []model.Invitation{
			{ID: 1, Email: "priya@example.test", ExpiresAt: fixedNow.Add(5 * 24 * hour), CreatedAt: ago(days(2))},
			{ID: 2, Email: "jonas@example.test", ExpiresAt: fixedNow.Add(2 * 24 * hour), CreatedAt: ago(days(5)), AcceptedAt: timePtr(ago(days(4)))},
		},
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func adminUserRows() []model.AdminUserRow {
	mk := func(id int64, name, display, email string, super, totp bool, suspended bool) model.AdminUserRow {
		u := model.User{ID: id, Username: name, Name: display, Email: email, IsSuperadmin: super, TOTPEnabled: totp, CreatedAt: ago(days(300 - int(id)*20)), EmailVerifiedAt: timePtr(ago(days(250)))}
		if suspended {
			u.SuspendedAt = timePtr(ago(days(15)))
		}
		return model.AdminUserRow{User: u, HasPassword: true}
	}
	return []model.AdminUserRow{
		mk(1, fixtureUser, "Malith Kuruppu", "mk@example.test", true, true, false),
		mk(2, "ada-silva", "Ada Silva", "ada@example.test", false, true, false),
		mk(3, "jonas-weber", "Jonas Weber", "jonas@example.test", false, false, false),
		mk(4, "priya-nair", "Priya Nair", "priya@example.test", false, false, true),
	}
}

func adminUsersData(b view.BasePage, rows []model.AdminUserRow) view.AdminUsersData {
	return view.AdminUsersData{BasePage: b, Users: rows, TotalCount: len(rows), Page: 1, PerPage: 50}
}

func adminUserData(b view.BasePage, row model.AdminUserRow, self bool) view.AdminUserData {
	d := service.AdminUserDetail{AdminUserRow: row, LastSignIn: timePtr(ago(days(1)))}
	if row.User.ID == 2 {
		d.SoleOwnedOrgs = []string{"tidepool"}
	}
	return view.AdminUserData{BasePage: b, Detail: d, Self: self, Confirm: passwordFactor}
}

func fixtureAudit() []model.AuditEntry {
	id := int64(1)
	return []model.AuditEntry{
		{ID: 3, ActorID: &id, ActorName: fixtureUser, Action: model.AuditActionRepoCreate, TargetType: model.AuditTargetRepo, TargetName: fixtureUser + "/hollow-atlas", IPAddress: "203.0.113.7", CreatedAt: ago(days(9))},
		{ID: 2, ActorID: &id, ActorName: fixtureUser, Action: model.AuditActionLogin, IPAddress: "203.0.113.7", CreatedAt: ago(days(10))},
		{ID: 1, ActorName: "ada-silva", Action: model.AuditActionRepoTransferRequest, TargetType: model.AuditTargetRepo, TargetName: "ada-silva/field-notes", IPAddress: "198.51.100.4", CreatedAt: ago(days(12))},
	}
}

func ldapConfig(enabled bool) *model.SSOConfig {
	return &model.SSOConfig{ID: 1, Provider: "ldap", Enabled: enabled, Config: map[string]string{
		"host": "ldap.example.test", "port": "636", "base_dn": "dc=example,dc=test", "bind_dn_tmpl": "uid=%s,ou=people,dc=example,dc=test", "use_tls": "true",
	}}
}

func samlConfig(enabled bool) *model.SSOConfig {
	return &model.SSOConfig{ID: 2, Provider: "saml", Enabled: enabled, Config: map[string]string{
		"entity_id": "https://cloudzilla.example.test/saml", "sso_url": "https://idp.example.test/sso", "acs_url": "https://cloudzilla.example.test/auth/saml/acs", "idp_cert": "MIIC...",
	}}
}
