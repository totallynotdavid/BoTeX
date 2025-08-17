package auth

import (
	"encoding/json"
	"time"
)

// User represents a unified user model combining auth and tier functionality
type User struct {
	UserID       string    `db:"user_id" json:"user_id"`
	Rank         string    `db:"rank" json:"rank"`
	RegisteredAt time.Time `db:"registered_at" json:"registered_at"`
	RegisteredBy *string   `db:"registered_by" json:"registered_by,omitempty"`
	Active       bool      `db:"active" json:"active"`
}

// Rank represents a user rank with associated permissions
type Rank struct {
	Name        string    `db:"name" json:"name"`
	Level       int       `db:"level" json:"level"`
	Commands    []string  `db:"-" json:"commands"`        // Parsed from JSON
	CommandsRaw string    `db:"commands" json:"-"`        // Raw JSON storage
	Description string    `db:"description" json:"description"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	Active      bool      `db:"active" json:"active"`
}

// MarshalCommands converts the Commands slice to JSON for database storage
func (r *Rank) MarshalCommands() error {
	if r.Commands == nil {
		r.CommandsRaw = "[]"
		return nil
	}
	
	data, err := json.Marshal(r.Commands)
	if err != nil {
		return err
	}
	r.CommandsRaw = string(data)
	return nil
}

// UnmarshalCommands converts the JSON CommandsRaw to Commands slice
func (r *Rank) UnmarshalCommands() error {
	if r.CommandsRaw == "" {
		r.Commands = []string{}
		return nil
	}
	
	return json.Unmarshal([]byte(r.CommandsRaw), &r.Commands)
}

// HasCommand checks if the rank has permission for a specific command
func (r *Rank) HasCommand(command string) bool {
	for _, cmd := range r.Commands {
		if cmd == command {
			return true
		}
	}
	return false
}

// RegisteredGroup represents a group registered in the auth system
type RegisteredGroup struct {
	GroupID      string    `db:"group_id" json:"group_id"`
	RegisteredAt time.Time `db:"registered_at" json:"registered_at"`
	RegisteredBy string    `db:"registered_by" json:"registered_by"`
	Active       bool      `db:"active" json:"active"`
}

// PermissionResult contains the result of a unified permission check
type PermissionResult struct {
	Allowed         bool   `json:"allowed"`
	Reason          string `json:"reason"`
	UserRank        string `json:"user_rank,omitempty"`
	IsWhatsAppAdmin bool   `json:"is_whatsapp_admin"`
}



// Default ranks that will be created during schema initialization
var DefaultRanks = []*Rank{
	{
		Name:        "owner",
		Level:       0,
		Commands:    []string{}, // Empty - owner must manually configure
		Description: "Bot owner with full administrative access",
		Active:      true,
	},
	{
		Name:        "basic",
		Level:       100,
		Commands:    []string{}, // Empty - owner must manually configure
		Description: "Basic access level for users",
		Active:      true,
	},
}