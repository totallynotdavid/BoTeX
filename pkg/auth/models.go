package auth

import (
	"time"
)

// Rank represents a user rank with specific privileges.
type Rank struct {
	ID          int    `db:"id"          json:"id"`
	Name        string `db:"name"        json:"name"`
	Description string `db:"description" json:"description"`
	Priority    int    `db:"priority"    json:"priority"`
	IsDefault   bool   `db:"is_default"  json:"isDefault"`
}

// Permission represents a specific action or access right.
type Permission struct {
	ID           int    `db:"id"            json:"id"`
	Name         string `db:"name"          json:"name"`
	Description  string `db:"description"   json:"description"`
	ResourceCost int    `db:"resource_cost" json:"resourceCost"`
}

// RankPermission represents the association between a rank and its permissions.
type RankPermission struct {
	RankID       int       `db:"rank_id"       json:"rankId"`
	PermissionID int       `db:"permission_id" json:"permissionId"`
	ExpiresAt    time.Time `db:"expires_at"    json:"expiresAt"`
}

// User represents a WhatsApp user with their rank and metadata.
type User struct {
	JID       string    `db:"jid"        json:"jid"`
	RankID    int       `db:"rank_id"    json:"rankId"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
}
