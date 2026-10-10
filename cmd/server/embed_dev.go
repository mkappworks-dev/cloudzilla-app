//go:build dev

package main

import (
	"io/fs"
	"os"
)

// The dev build reads the frontend from the working tree, so editing CSS or JS
// needs no rebuild. router.New does fs.Sub(frontend, "frontend"), so this root
// matches the embed root in embed.go — which means running from the repo root.
var frontendFS fs.FS = os.DirFS("cmd/server")
