package model

type SiteSetting struct {
	Key   string `db:"key"   json:"key"`
	Value string `db:"value" json:"value"`
}
