//go:build !dev

package layout

import "github.com/a-h/templ"

// A release build carries no live-reload client.
func devReload() templ.Component { return templ.NopComponent }
