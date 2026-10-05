// Command gen-code-themes writes the code theme stylesheet the server embeds.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
)

func main() {
	out := flag.String("o", "cmd/server/frontend/static/code-themes.css", "output path")
	flag.Parse()
	if err := os.WriteFile(*out, highlight.Stylesheet(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-code-themes:", err)
		os.Exit(1)
	}
}
