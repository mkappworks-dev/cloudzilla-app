package db_test

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// The runner records applied migrations by full filename, so renaming one of
// these would re-run it on every deployed database. They stay as they are.
var legacySharedPrefixes = map[string]bool{
	"036": true,
	"102": true,
}

func TestMigrations_NumericPrefixesAreUnique(t *testing.T) {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	byPrefix := map[string][]string{}
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			t.Errorf("%s has no numeric prefix", e.Name())
			continue
		}
		byPrefix[prefix] = append(byPrefix[prefix], e.Name())
	}

	var prefixes []string
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		files := byPrefix[p]
		switch {
		case len(files) > 1 && !legacySharedPrefixes[p]:
			t.Errorf("migrations share the number %s: %s; give the newer one the next free number", p, strings.Join(files, ", "))
		case len(files) == 1 && legacySharedPrefixes[p]:
			t.Errorf("number %s is no longer shared; remove it from legacySharedPrefixes", p)
		}
	}
}
