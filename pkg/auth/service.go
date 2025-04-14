package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"botex/pkg/logger"
)

var (
	ErrRankNotFound       = errors.New("rank not found")
	ErrPermissionNotFound = errors.New("permission not found")
	ErrUserNotFound       = errors.New("user not found")
	ErrAccessDenied       = errors.New("access denied")
)

// Service handles all authentication and authorization operations.
type Service struct {
	db     *sql.DB
	logger *logger.Logger
}

// NewService creates a new auth service.
func NewService(database *sql.DB, loggerFactory *logger.LoggerFactory) (*Service, error) {
	if err := InitDB(context.Background(), database); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &Service{
		db:     database,
		logger: loggerFactory.GetLogger("auth-service"),
	}, nil
}

// GetUserRank returns the rank of a user.
func (s *Service) GetUserRank(ctx context.Context, jid string) (*Rank, error) {
	var rank Rank
	query := `
		SELECT r.id, r.name, r.description, r.priority, r.is_default
		FROM botex_users u
		JOIN botex_ranks r ON u.rank_id = r.id
		WHERE u.jid = ?
	`
	err := s.db.QueryRowContext(ctx, query, jid).Scan(
		&rank.ID, &rank.Name, &rank.Description, &rank.Priority, &rank.IsDefault,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}

		return nil, fmt.Errorf("failed to get user rank: %w", err)
	}

	return &rank, nil
}

// HasPermission checks if a user has a specific permission.
func (s *Service) HasPermission(ctx context.Context, jid, permissionName string) (bool, error) {
	query := `
		SELECT COUNT(*) > 0
		FROM botex_users u
		JOIN botex_rank_permissions rp ON u.rank_id = rp.rank_id
		JOIN botex_permissions p ON rp.permission_id = p.id
		WHERE u.jid = ? AND p.name = ? AND (rp.expires_at IS NULL OR rp.expires_at > ?)
	`
	var hasPermission bool
	err := s.db.QueryRowContext(ctx, query, jid, permissionName, time.Now()).Scan(&hasPermission)
	if err != nil {
		return false, fmt.Errorf("failed to check permission: %w", err)
	}

	return hasPermission, nil
}

// GetUserPermissions returns all permissions for a user.
func (s *Service) GetUserPermissions(ctx context.Context, jid string) ([]Permission, error) {
	query := `
		SELECT p.id, p.name, p.description, p.resource_cost
		FROM botex_users u
		JOIN botex_rank_permissions rp ON u.rank_id = rp.rank_id
		JOIN botex_permissions p ON rp.permission_id = p.id
		WHERE u.jid = ? AND (rp.expires_at IS NULL OR rp.expires_at > ?)
	`
	rows, err := s.db.QueryContext(ctx, query, jid, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query user permissions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			s.logger.Error("failed to close rows", map[string]interface{}{
				"error": err,
			})
		}
	}()

	var permissions []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.ResourceCost); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating permissions: %w", err)
	}

	return permissions, nil
}

// SetUserRank updates a user's rank.
func (s *Service) SetUserRank(ctx context.Context, jid string, rankID int) error {
	query := `
		INSERT INTO botex_users (jid, rank_id, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			rank_id = ?,
			updated_at = ?
	`
	now := time.Now()
	_, err := s.db.ExecContext(ctx, query,
		jid, rankID, now, now,
		rankID, now,
	)
	if err != nil {
		return fmt.Errorf("failed to set user rank: %w", err)
	}

	return nil
}

// CheckResourceAccess checks if a user can access a resource based on their permissions.
func (s *Service) CheckResourceAccess(ctx context.Context, jid, permissionName string, resourceCost int) (bool, error) {
	query := `
		SELECT p.resource_cost <= (
			SELECT COALESCE(SUM(p2.resource_cost), 0)
			FROM botex_users u
			JOIN botex_rank_permissions rp ON u.rank_id = rp.rank_id
			JOIN botex_permissions p2 ON rp.permission_id = p2.id
			WHERE u.jid = ? AND p2.name = ? AND (rp.expires_at IS NULL OR rp.expires_at > ?)
		)
		FROM botex_permissions p
		WHERE p.name = ?
	`
	var hasAccess bool
	err := s.db.QueryRowContext(ctx, query, jid, permissionName, time.Now(), permissionName).Scan(&hasAccess)
	if err != nil {
		return false, fmt.Errorf("failed to check resource access: %w", err)
	}

	return hasAccess, nil
}

// GetRankByName returns a rank by its name.
func (s *Service) GetRankByName(ctx context.Context, name string) (*Rank, error) {
	var rank Rank
	query := `
		SELECT id, name, description, priority, is_default
		FROM botex_ranks
		WHERE name = ?
	`
	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&rank.ID, &rank.Name, &rank.Description, &rank.Priority, &rank.IsDefault,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRankNotFound
		}

		return nil, fmt.Errorf("failed to get rank by name: %w", err)
	}

	return &rank, nil
}

// GetAllRanks returns all available ranks.
func (s *Service) GetAllRanks(ctx context.Context) ([]Rank, error) {
	query := `
		SELECT id, name, description, priority, is_default
		FROM botex_ranks
		ORDER BY priority DESC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query ranks: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			s.logger.Error("failed to close rows", map[string]interface{}{
				"error": err,
			})
		}
	}()

	var ranks []Rank
	for rows.Next() {
		var rank Rank
		if err := rows.Scan(
			&rank.ID, &rank.Name, &rank.Description, &rank.Priority, &rank.IsDefault,
		); err != nil {
			return nil, fmt.Errorf("failed to scan rank: %w", err)
		}
		ranks = append(ranks, rank)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ranks: %w", err)
	}

	return ranks, nil
}
