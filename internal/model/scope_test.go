package model

import "testing"

func TestPackageScopes(t *testing.T) {
	for _, sc := range []string{ScopePackagesRead, ScopePackagesWrite} {
		if !IsTokenScope(sc) || !IsDeviceScope(sc) {
			t.Errorf("%s must be accepted for tokens and device grants", sc)
		}
		if IsKnownScope(sc) {
			t.Errorf("%s must not be an OAuth scope", sc)
		}
		if ScopeDescription(sc) == "" {
			t.Errorf("%s has no description", sc)
		}
	}
	for _, sc := range Scopes {
		if !IsDeviceScope(sc) || !IsTokenScope(sc) {
			t.Errorf("%s must stay valid for tokens and device grants", sc)
		}
	}
	if IsDeviceScope(ScopeRepoAdmin) || IsDeviceScope("nope") {
		t.Error("repo:admin and unknown scopes are not device scopes")
	}
	for _, sc := range Scopes {
		if sc == ScopePackagesRead || sc == ScopePackagesWrite {
			t.Errorf("Scopes is the OAuth consent list and must not hold %s", sc)
		}
	}
}
