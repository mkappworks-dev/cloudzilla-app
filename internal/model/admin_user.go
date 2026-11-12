package model

// AdminUserRow is an account as the admin user pages show it.
type AdminUserRow struct {
	User        User
	HasPassword bool
	// SSOProvider is "ldap" or "saml" for an account linked to one, else "".
	SSOProvider string
}

const (
	AdminUserRoleSuperadmin = "superadmin"
	AdminUserRoleUser       = "user"

	AdminUserStatusActive    = "active"
	AdminUserStatusSuspended = "suspended"
)

// AdminUserFilter narrows /admin/users. Empty fields match everything.
type AdminUserFilter struct {
	// Query matches a username or email prefix, ignoring case.
	Query  string
	Role   string
	Status string
	// Sort is one of AdminUserSort's keys; "" orders by created.
	Sort string
	Desc bool
}

// AdminUserSort lists the columns /admin/users sorts by.
var AdminUserSort = SortSpec{{Key: "created", DescFirst: true}, {Key: "username"}, {Key: "email"}}

// RevokedCredentials counts what an admin's "revoke tokens and keys" deleted.
type RevokedCredentials struct {
	AccessTokens        int64
	SSHKeys             int64
	OAuthAuthorizations int64
}
