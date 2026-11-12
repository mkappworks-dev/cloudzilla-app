package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// Viewer is who the variant is drawn for.
type Viewer string

const (
	Anon   Viewer = "anon"
	Reader Viewer = "viewer" // signed in, read-only
	Writer Viewer = "writer"
	Admin  Viewer = "admin"
)

type variant struct {
	// Name is the file suffix after "--"; empty for the default variant.
	Name   string
	Note   string
	Viewer Viewer
	// AdminMode draws the "Acting as instance admin" banner.
	AdminMode bool
	Render    func(base view.BasePage) templ.Component
}

type page struct {
	Name  string // file stem of the default variant
	Route string
	Templ string // source file under internal/view/pages
	Title string
	// Variants lists the default first. A role variant exists only where the templ branches on that role.
	Variants []variant
}

func fileStem(p page, v variant) string {
	if v.Name == "" {
		return p.Name
	}
	return p.Name + "--" + v.Name
}

func registry() []page {
	all := []page{
		{
			Name: "home", Route: "/", Templ: "home.templ", Title: "Home",
			Variants: []variant{
				{Note: "signed in with repositories", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Home(homeData(b))
				}},
				{Name: "anon", Note: "signed out", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.Home(view.HomeData{BasePage: b})
				}},
				{Name: "empty", Note: "no repositories yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Home(view.HomeData{BasePage: onAccountPage(b, "overview"), RepoSort: "updated", RepoFilter: "all"})
				}},
				{Name: "admin-mode", Note: "instance admin acting with elevated rights", Viewer: Admin, AdminMode: true,
					Render: func(b view.BasePage) templ.Component { return pages.Home(homeData(b)) }},
			},
		},
		{
			Name: "login", Route: "/login", Templ: "login.templ", Title: "Sign in",
			Variants: []variant{
				{Note: "email and password; the instance cannot send email, so no reset link", Viewer: Anon,
					Render: func(b view.BasePage) templ.Component {
						return pages.Login(view.LoginData{BasePage: b, AllowRegistration: true})
					}},
				{Name: "reset-link", Note: "the instance can send email", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.Login(view.LoginData{BasePage: b, AllowRegistration: true, ForgotPassword: true})
				}},
				{Name: "error", Note: "wrong password", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.Login(view.LoginData{BasePage: b, AllowRegistration: true, Error: "Invalid email or password."})
				}},
				{Name: "sso", Note: "LDAP and SAML enabled", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.Login(view.LoginData{BasePage: b, LDAPEnabled: true, SAMLEnabled: true})
				}},
			},
		},
		{
			Name: "not_found", Route: "any 404", Templ: "not_found.templ", Title: "Page not found",
			Variants: []variant{
				{Note: "signed in", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.NotFound(b)
				}},
				{Name: "anon", Note: "signed out", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.NotFound(b)
				}},
			},
		},
	}
	return append(all, accountPages()...)
}
