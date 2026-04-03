package model

import "time"

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
	LDAPKeyPort       = "port"           // default "389"
	LDAPKeyBaseDN     = "base_dn"        // e.g. "dc=example,dc=com"
	LDAPKeyBindDNTmpl = "bind_dn_tmpl"   // e.g. "uid=%s,ou=people,dc=example,dc=com"
	LDAPKeyUseTLS     = "use_tls"        // "true" / "false"
)

// SAML config map keys.
const (
	SAMLKeyEntityID    = "entity_id"
	SAMLKeyMetadataURL = "metadata_url"
	SAMLKeyACSURL      = "acs_url"    // Assertion Consumer Service URL (our endpoint)
	SAMLKeyCert        = "idp_cert"   // PEM-encoded IdP signing certificate (base64, no headers)
)
