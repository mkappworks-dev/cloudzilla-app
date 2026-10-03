package model

import (
	"strings"
	"time"
)

// SSOConfig represents a single provider's configuration row in sso_configs.
type SSOConfig struct {
	ID        int64             `db:"id"         json:"id"`
	Provider  string            `db:"provider"   json:"provider"`
	Config    map[string]string `db:"-"          json:"config"`
	Enabled   bool              `db:"enabled"    json:"enabled"`
	CreatedAt time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt time.Time         `db:"updated_at" json:"updated_at"`
}

// LDAP config map keys.
const (
	LDAPKeyHost       = "host"
	LDAPKeyPort       = "port"         // default "389"
	LDAPKeyBaseDN     = "base_dn"      // e.g. "dc=example,dc=com"
	LDAPKeyBindDNTmpl = "bind_dn_tmpl" // e.g. "uid=%s,ou=people,dc=example,dc=com"
	LDAPKeyUseTLS     = "use_tls"      // "true" / "false"
)

// SAML config map keys.
const (
	SAMLKeyEntityID    = "entity_id"
	SAMLKeyMetadataURL = "metadata_url" // where the settings form once saved the IdP SSO URL; only shown as a fallback
	SAMLKeySSOURL      = "sso_url"      // IdP Single Sign-On endpoint (redirect target for AuthnRequest)
	SAMLKeyACSURL      = "acs_url"      // Assertion Consumer Service URL (our endpoint)
	SAMLKeyCert        = "idp_cert"     // PEM-encoded IdP signing certificate (base64, no headers)
)

// ssoRequiredKeys are the settings each provider's sign-in fails without, in
// the order the settings page shows their fields.
var ssoRequiredKeys = map[string][]string{
	"ldap": {LDAPKeyHost, LDAPKeyBindDNTmpl},
	"saml": {SAMLKeyEntityID, SAMLKeySSOURL, SAMLKeyACSURL, SAMLKeyCert},
}

// SSOMissingSettings lists the settings signing in with provider needs that
// config leaves blank.
func SSOMissingSettings(provider string, config map[string]string) []string {
	var missing []string
	for _, k := range ssoRequiredKeys[provider] {
		if strings.TrimSpace(config[k]) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}

// SSOSettingsReady reports whether config has every setting signing in with
// provider needs, which it must before the provider can be on.
func SSOSettingsReady(provider string, config map[string]string) bool {
	_, known := ssoRequiredKeys[provider]
	return known && len(SSOMissingSettings(provider, config)) == 0
}

// Ready reports whether c's saved settings let its provider be turned on.
func (c *SSOConfig) Ready() bool {
	return c != nil && SSOSettingsReady(c.Provider, c.Config)
}
