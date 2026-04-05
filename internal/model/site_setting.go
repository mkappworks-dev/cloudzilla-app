package model

// SiteSetting represents an instance-wide configuration key-value pair.
type SiteSetting struct {
	Key   string `db:"key"   json:"key"`
	Value string `db:"value" json:"value"`
}
