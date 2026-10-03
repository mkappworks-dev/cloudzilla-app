package model_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestSSOConfig_Ready(t *testing.T) {
	ldap := map[string]string{model.LDAPKeyHost: "ldap.test.invalid", model.LDAPKeyBindDNTmpl: "uid=%s,dc=test"}
	saml := map[string]string{
		model.SAMLKeyEntityID: "cz", model.SAMLKeySSOURL: "https://idp.test.invalid/sso",
		model.SAMLKeyACSURL: "https://cz.test.invalid/acs", model.SAMLKeyCert: "MIIB",
	}
	without := func(cfg map[string]string, key string) map[string]string {
		out := map[string]string{}
		for k, v := range cfg {
			if k != key {
				out[k] = v
			}
		}
		return out
	}

	for _, tc := range []struct {
		name string
		cfg  *model.SSOConfig
		want bool
	}{
		{"nothing saved", nil, false},
		{"ldap", &model.SSOConfig{Provider: "ldap", Config: ldap}, true},
		{"ldap without host", &model.SSOConfig{Provider: "ldap", Config: without(ldap, model.LDAPKeyHost)}, false},
		{"ldap without bind DN template", &model.SSOConfig{Provider: "ldap", Config: without(ldap, model.LDAPKeyBindDNTmpl)}, false},
		{"ldap with a blank host", &model.SSOConfig{Provider: "ldap", Config: map[string]string{model.LDAPKeyHost: "  ", model.LDAPKeyBindDNTmpl: "uid=%s"}}, false},
		{"saml", &model.SSOConfig{Provider: "saml", Config: saml}, true},
		{"saml without SSO URL", &model.SSOConfig{Provider: "saml", Config: without(saml, model.SAMLKeySSOURL)}, false},
		{"saml without certificate", &model.SSOConfig{Provider: "saml", Config: without(saml, model.SAMLKeyCert)}, false},
		{"unknown provider", &model.SSOConfig{Provider: "oidc", Config: ldap}, false},
	} {
		if got := tc.cfg.Ready(); got != tc.want {
			t.Errorf("%s: Ready() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
