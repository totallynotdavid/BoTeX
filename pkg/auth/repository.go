package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// user operations.
func (r *Repository) GetUser(ctx context.Context, userID string) (*User, error) {
	query := `SELECT user_id, rank, registered_at, registered_by 
			  FROM users WHERE user_id = ? AND active = 1`

	var (
		user         User
		registeredBy sql.NullString
	)

	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&user.ID, &user.Rank, &user.RegisteredAt, &registeredBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}

		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if registeredBy.Valid {
		user.RegisteredBy = registeredBy.String
	}

	return &user, nil
}

func (r *Repository) CreateUser(ctx context.Context, userID, rank, registeredBy string) error {
	query := `INSERT INTO users (user_id, rank, registered_by, active) 
			  VALUES (?, ?, ?, 1)`

	_, err := r.db.ExecContext(ctx, query, userID, rank, registeredBy)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

func (r *Repository) UserExists(ctx context.Context, userID string) (bool, error) {
	query := `SELECT 1 FROM users WHERE user_id = ? AND active = 1`

	var exists int

	err := r.db.QueryRowContext(ctx, query, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("failed to check user existence: %w", err)
	}

	return true, nil
}

// rank operations.
func (r *Repository) GetRank(ctx context.Context, name string) (*Rank, error) {
	query := `SELECT name, level, commands FROM ranks WHERE name = ? AND active = 1`

	var (
		rank        Rank
		commandsRaw string
	)

	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&rank.Name, &rank.Level, &commandsRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRankNotFound
		}

		return nil, fmt.Errorf("failed to get rank: %w", err)
	}

	rank.Commands = ParseCommands(commandsRaw)

	return &rank, nil
}

func (r *Repository) ListRanks(ctx context.Context) ([]*Rank, error) {
	query := `SELECT name, level, commands FROM ranks WHERE active = 1 ORDER BY level`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list ranks: %w", err)
	}

	defer rows.Close()

	var ranks []*Rank
	for rows.Next() {
		var (
			rank        Rank
			commandsRaw string
		)

		err := rows.Scan(&rank.Name, &rank.Level, &commandsRaw)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rank: %w", err)
		}

		rank.Commands = ParseCommands(commandsRaw)
		ranks = append(ranks, &rank)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ranks: %w", err)
	}

	return ranks, nil
}

// group operations.
func (r *Repository) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	query := `SELECT group_id, registered_at, registered_by 
			  FROM registered_groups WHERE group_id = ? AND active = 1`

	var group Group

	err := r.db.QueryRowContext(ctx, query, groupID).Scan(
		&group.ID, &group.RegisteredAt, &group.RegisteredBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGroupNotRegistered
		}

		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

func (r *Repository) CreateGroup(ctx context.Context, groupID, registeredBy string) error {
	query := `INSERT INTO registered_groups (group_id, registered_by, active) 
			  VALUES (?, ?, 1)`

	_, err := r.db.ExecContext(ctx, query, groupID, registeredBy)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	return nil
}

func (r *Repository) GroupExists(ctx context.Context, groupID string) (bool, error) {
	query := `SELECT 1 FROM registered_groups WHERE group_id = ? AND active = 1`

	var exists int

	err := r.db.QueryRowContext(ctx, query, groupID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("failed to check group existence: %w", err)
	}

	return true, nil
}
