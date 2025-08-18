package auth

import (
	"context"
	"database/sql"
)

type Auth interface {
	CheckPermission(ctx context.Context, userID, groupID, command string) (*PermissionResult, error)
	RegisterUser(ctx context.Context, userID, rank, registeredBy string) error
	RegisterGroup(ctx context.Context, groupID, registeredBy string) error
	GetUser(ctx context.Context, userID string) (*User, error)
	GetRank(ctx context.Context, rankName string) (*Rank, error)
	GetGroup(ctx context.Context, groupID string) (*Group, error)
	ListRanks(ctx context.Context) ([]*Rank, error)
}

func New(db *sql.DB) *Service {
	return NewService(db)
}
