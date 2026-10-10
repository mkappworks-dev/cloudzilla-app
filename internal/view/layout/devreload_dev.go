//go:build dev

package layout

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// The dev build loads the reload client, served by probeMux at /_dev/reload.js.
// Only the layout needs it: HTMX fragments render without it, so the page never
// ends up with two streams.
func devReload() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<script src="/_dev/reload.js" defer></script>`)
		return err
	})
}
