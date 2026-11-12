// Command mockgen renders the shipped page components with fixture view-models into mockups/generated/, so the standing
// mocks cannot drift from the templates. It is a separate binary: the server never links it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", "mockups", "mockups directory to write generated/, index.html and COVERAGE.md into")
	flag.Parse()
	if err := generate(context.Background(), *out, registry()); err != nil {
		fmt.Fprintln(os.Stderr, "mockgen:", err)
		os.Exit(1)
	}
}

func generate(ctx context.Context, dir string, pages []page) error {
	// Render everything before touching disk, so a failing variant cannot leave the committed mocks emptied.
	rendered := map[string][]byte{}
	for _, p := range pages {
		for _, v := range p.Variants {
			html, err := renderVariant(ctx, v)
			if err != nil {
				return fmt.Errorf("%s: %w", fileStem(p, v), err)
			}
			rendered[fileStem(p, v)+".html"] = html
		}
	}
	genDir := filepath.Join(dir, "generated")
	// Clear first so a renamed or removed variant leaves no stale file for CI to miss.
	if err := os.RemoveAll(genDir); err != nil {
		return err
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return err
	}
	for name, html := range rendered {
		if err := os.WriteFile(filepath.Join(genDir, name), html, 0o644); err != nil {
			return err
		}
	}
	if err := rewriteRegion(filepath.Join(dir, "index.html"), indexRegion(pages)); err != nil {
		return err
	}
	return rewriteRegion(filepath.Join(dir, "COVERAGE.md"), coverageRegion(pages))
}
