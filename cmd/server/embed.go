//go:build !dev

package main

import "embed"

//go:embed all:frontend
var frontendFS embed.FS
