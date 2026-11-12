package pages

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// adminUserSignIn names the ways an account can sign in.
func adminUserSignIn(r model.AdminUserRow) string {
	var ways []string
	if r.HasPassword {
		ways = append(ways, "Password")
	}
	if r.User.OAuthProvider == "google" {
		ways = append(ways, "Google")
	}
	switch r.SSOProvider {
	case "ldap":
		ways = append(ways, "LDAP")
	case "saml":
		ways = append(ways, "SAML")
	}
	if len(ways) == 0 {
		return "None"
	}
	return strings.Join(ways, ", ")
}

func adminUsersPageURL(f model.AdminUserFilter, page int) string {
	q := url.Values{"page": {strconv.Itoa(page)}}
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Role != "" {
		q.Set("role", f.Role)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
		q.Set("dir", sortDir(f.Desc))
	}
	return "/admin/users?" + q.Encode()
}

// adminUsersSortURL is the header link for key: page 1 of the same filter, sorted by key.
func adminUsersSortURL(f model.AdminUserFilter, key string) string {
	cur, desc := adminUsersSortState(f)
	f.Desc = model.AdminUserSort.Toggle(cur, desc, key)
	f.Sort = key
	return adminUsersPageURL(f, 1)
}

// ariaSort is the aria-sort value for a column: "" unless it is the sorted one.
func ariaSort(sorted, key string, desc bool) string {
	switch {
	case sorted != key:
		return ""
	case desc:
		return "descending"
	}
	return "ascending"
}

func sortDir(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

func adminUserAction(username, action string) string {
	return "/api/admin/users/" + url.PathEscape(username) + "/" + action
}

func adminOrgOwners(org string) string {
	return "/api/admin/orgs/" + url.PathEscape(org) + "/owners"
}

// adminSuspendMessage warns about the orgs the user is the only owner of,
// which nobody can administer while the account is suspended.
func adminSuspendMessage(username string, soleOwnedOrgs []string) string {
	msg := "@" + username + " will be signed out everywhere and can't sign in, push or use tokens until unsuspended."
	if len(soleOwnedOrgs) > 0 {
		msg += " They are the only owner of " + strings.Join(soleOwnedOrgs, ", ") + ", which will have no active owner meanwhile."
	}
	return msg
}

// adminUsersSortState is the sorted column and direction, which a filter built outside the handler leaves empty.
func adminUsersSortState(f model.AdminUserFilter) (string, bool) {
	if f.Sort == "" {
		return model.AdminUserSort[0].Key, model.AdminUserSort[0].DescFirst
	}
	return f.Sort, f.Desc
}

func adminUsersAriaSort(f model.AdminUserFilter, key string) string {
	cur, desc := adminUsersSortState(f)
	return ariaSort(cur, key, desc)
}
