package config_test

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// A key the page names that no default backs is a typo that silently does nothing; a default the page never names
// is a knob an operator cannot find.
func TestPackageRegistryDoc_NamesExactlyTheRegistryPackageAndQuotaKeys(t *testing.T) {
	doc, err := os.ReadFile("../../docs/package-registry.md")
	if err != nil {
		t.Fatalf("read the page: %v", err)
	}
	// The leading class keeps `rate_limit.registry.*`, documented in configuration.md, from matching as `registry.*`.
	re := regexp.MustCompile("`((?:registry|packages)\\.[a-z_]+|quota\\.(?:user|org)\\.package_bytes)`")
	var named []string
	for _, m := range re.FindAllStringSubmatch(string(doc), -1) {
		if !slices.Contains(named, m[1]) {
			named = append(named, m[1])
		}
	}

	var cfg config.Config
	var defaults []string
	defaults = append(defaults, keysOf(reflect.TypeOf(cfg.Registry), "registry")...)
	defaults = append(defaults, keysOf(reflect.TypeOf(cfg.Packages), "packages")...)
	defaults = append(defaults, "quota.user.package_bytes", "quota.org.package_bytes")

	for _, k := range named {
		if !slices.Contains(defaults, k) {
			t.Errorf("docs/package-registry.md names %s, which has no config default", k)
		}
	}
	for _, k := range defaults {
		if !slices.Contains(named, k) {
			t.Errorf("config default %s is not mentioned in docs/package-registry.md", k)
		}
	}
}

func keysOf(t reflect.Type, prefix string) []string {
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		keys = append(keys, prefix+"."+t.Field(i).Tag.Get("mapstructure"))
	}
	return keys
}
