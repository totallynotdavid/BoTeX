package auth

import "context"

// AuthService defines the unified authentication and authorization service interface
type AuthService interface {
	// Permission checking (primary use case)
	CheckPermission(ctx context.Context, userID, groupID, command string) (*PermissionResult, error)
	
	// User management
	RegisterUser(ctx context.Context, userID, rank string) error
	UnregisterUser(ctx context.Context, userID string) error
	GetUser(ctx context.Context, userID string) (*User, error)
	SetUserRank(ctx context.Context, userID, rank string) error
	
	// Group management
	RegisterGroup(ctx context.Context, groupID, registeredBy string) error
	UnregisterGroup(ctx context.Context, groupID string) error
	IsGroupRegistered(ctx context.Context, groupID string) (bool, error)
	
	// Rank management (for future API)
	GetRank(ctx context.Context, rankName string) (*Rank, error)
	CreateRank(ctx context.Context, rank *Rank) error
	UpdateRank(ctx context.Context, rank *Rank) error
	ListRanks(ctx context.Context) ([]*Rank, error)
	DeleteRank(ctx context.Context, rankName string) error
	InitializeDefaultRanks(ctx context.Context) error
	
	// Health
	Close() error
}

// AuthStore defines the database operations interface for the auth system
type AuthStore interface {
	// User operations
	GetUser(ctx context.Context, userID string) (*User, error)
	CreateUser(ctx context.Context, user *User) error
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, userID string) error
	
	// Rank operations
	GetRank(ctx context.Context, rankName string) (*Rank, error)
	CreateRank(ctx context.Context, rank *Rank) error
	UpdateRank(ctx context.Context, rank *Rank) error
	ListRanks(ctx context.Context) ([]*Rank, error)
	CountUsersWithRank(ctx context.Context, rankName string) (int, error)
	
	// Group operations
	GetGroup(ctx context.Context, groupID string) (*RegisteredGroup, error)
	CreateGroup(ctx context.Context, group *RegisteredGroup) error
	DeleteGroup(ctx context.Context, groupID string) error
	
	// WhatsApp admin checking
	IsWhatsAppAdmin(ctx context.Context, userID, groupID string) (bool, error)
	
	Close() error
}