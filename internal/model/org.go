package model

import "time"

type OrgRole string

const (
	OrgRoleOwner  OrgRole = "owner"
	OrgRoleMember OrgRole = "member"
)

type Organization struct {
	ID          int64     `db:"id"           json:"id"`
	Name        string    `db:"name"         json:"name"`
	DisplayName string    `db:"display_name" json:"display_name"`
	Description string    `db:"description"  json:"description"`
	AvatarURL   string    `db:"avatar_url"   json:"avatar_url"`
	CreatedAt   time.Time `db:"created_at"   json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"   json:"updated_at"`
}

type OrgMember struct {
	ID        int64     `db:"id"         json:"id"`
	OrgID     int64     `db:"org_id"     json:"org_id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	Username  string    `db:"username"   json:"username,omitempty"`
	Role      OrgRole   `db:"role"       json:"role"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
