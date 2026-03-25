package model

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// StringSlice is a []string that scans from / writes to a PostgreSQL TEXT[] literal.
type StringSlice []string

func (s *StringSlice) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("StringSlice: unsupported source type %T", src)
	}
	// strip surrounding braces
	raw = strings.TrimSpace(raw)
	if raw == "{}" {
		*s = []string{}
		return nil
	}
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	// naive split — context names must not contain commas (consistent with PostgreSQL context naming)
	*s = strings.Split(raw, ",")
	return nil
}

func (s StringSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return "{}", nil
	}
	return "{" + strings.Join(s, ",") + "}", nil
}

type BranchProtection struct {
	ID                  int64       `db:"id"                    json:"id"`
	RepoID              int64       `db:"repo_id"               json:"repo_id"`
	Pattern             string      `db:"pattern"               json:"pattern"`
	RequireReviewCount  int         `db:"require_review_count"  json:"require_review_count"`
	RequireStatusChecks StringSlice `db:"require_status_checks" json:"require_status_checks"`
	BlockForcePush      bool        `db:"block_force_push"      json:"block_force_push"`
	CreatedAt           time.Time   `db:"created_at"            json:"created_at"`
	UpdatedAt           time.Time   `db:"updated_at"            json:"updated_at"`
}
