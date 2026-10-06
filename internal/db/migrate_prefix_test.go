package db_test

import (
	"os"
	"sort"
	"strings"
	"testing"
)

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
		if len(files) > 1 {
			t.Errorf("migrations share the number %s: %s; give the newer one the next free number", p, strings.Join(files, ", "))
		}
	}
}
