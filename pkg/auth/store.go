package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"botex/pkg/logger"
)

// SQLiteAuthStore implements the unified AuthStore interface using SQLite database.
type SQLiteAuthStore struct {
	db             *sql.DB
	logger         *logger.Logger
	whatsappClient WhatsAppClient // For admin verification
	closed         bool
}

// WhatsAppClient interface for WhatsApp admin verification.
type WhatsAppClient interface {
	IsConnected() bool
	IsGroupAdmin(ctx context.Context, userJID, groupJID string) (bool, error)
}

// NewSQLiteAuthStore creates a new SQLite-based unified auth store.
func NewSQLiteAuthStore(db *sql.DB, loggerFactory *logger.Factory, whatsappClient WhatsAppClient) (*SQLiteAuthStore, error) {
	if db == nil {
		return nil, NewAuthError("store_init", errors.New("database connection cannot be nil"))
	}

	storeLogger := loggerFactory.GetLogger("auth-store")

	store := &SQLiteAuthStore{
		db:             db,
		logger:         storeLogger,
		whatsappClient: whatsappClient,
		closed:         false,
	}

	// Validate that the required tables exist
	err := store.validateSchema(context.Background())
	if err != nil {
		return nil, NewAuthError("store_init", fmt.Errorf("schema validation failed: %w", err))
	}

	storeLogger.Info("Unified auth store initialized successfully", nil)

	return store, nil
}

// GetUser retrieves a user by ID from the unified users table.
func (s *SQLiteAuthStore) GetUser(ctx context.Context, userID string) (*User, error) {
	if s.closed {
		return nil, NewUserAuthError("get_user", userID, ErrServiceClosed)
	}

	if userID == "" {
		return nil, NewUserAuthError("get_user", userID, ErrInvalidUserID)
	}

	s.logger.Debug("Getting user", map[string]interface{}{
		"user_id": userID,
	})

	query := `
		SELECT user_id, rank, registered_at, registered_by, active 
		FROM users 
		WHERE user_id = ? AND active = TRUE
	`

	var (
		user         User
		registeredBy sql.NullString
	)

	err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&user.UserID,
		&user.Rank,
		&user.RegisteredAt,
		&registeredBy,
		&user.Active,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewUserAuthError("get_user", userID, ErrUserNotFound)
	}

	if err != nil {
		s.logger.Error("Failed to get user", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})

		return nil, NewUserAuthError("get_user", userID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	if registeredBy.Valid {
		user.RegisteredBy = &registeredBy.String
	}

	s.logger.Debug("User retrieved successfully", map[string]interface{}{
		"user_id": userID,
		"rank":    user.Rank,
	})

	return &user, nil
}

// CreateUser creates a new user in the unified users table.
func (s *SQLiteAuthStore) CreateUser(ctx context.Context, user *User) error {
	if s.closed {
		return NewUserAuthError("create_user", user.UserID, ErrServiceClosed)
	}

	if user == nil {
		return NewAuthError("create_user", errors.New("user cannot be nil"))
	}

	if user.UserID == "" {
		return NewUserAuthError("create_user", user.UserID, ErrInvalidUserID)
	}

	if user.Rank == "" {
		user.Rank = "basic" // Default rank
	}

	s.logger.Debug("Creating user", map[string]interface{}{
		"user_id": user.UserID,
		"rank":    user.Rank,
	})

	// Check if user already exists
	existingUser, err := s.GetUser(ctx, user.UserID)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		// If it's not a "not found" error, it's a real database error
		var authErr *AuthError
		if errors.As(err, &authErr) && !errors.Is(authErr.Err, ErrUserNotFound) {
			return err
		}
	}

	if existingUser != nil {
		return NewUserAuthError("create_user", user.UserID, ErrUserAlreadyExists)
	}

	// Verify that the rank exists
	_, err = s.GetRank(ctx, user.Rank)
	if err != nil {
		return NewUserAuthError("create_user", user.UserID, fmt.Errorf("invalid rank %s: %w", user.Rank, err))
	}

	// Set defaults if not provided
	if user.RegisteredAt.IsZero() {
		user.RegisteredAt = time.Now()
	}

	query := `
		INSERT INTO users (user_id, rank, registered_at, registered_by, active)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		user.UserID,
		user.Rank,
		user.RegisteredAt,
		user.RegisteredBy,
		user.Active,
	)
	if err != nil {
		s.logger.Error("Failed to create user", map[string]interface{}{
			"user_id": user.UserID,
			"rank":    user.Rank,
			"error":   err.Error(),
		})

		return NewUserAuthError("create_user", user.UserID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Info("User created successfully", map[string]interface{}{
		"user_id": user.UserID,
		"rank":    user.Rank,
	})

	return nil
}

// UpdateUser updates an existing user's information.
func (s *SQLiteAuthStore) UpdateUser(ctx context.Context, user *User) error {
	if s.closed {
		return NewUserAuthError("update_user", user.UserID, ErrServiceClosed)
	}

	if user == nil {
		return NewAuthError("update_user", errors.New("user cannot be nil"))
	}

	if user.UserID == "" {
		return NewUserAuthError("update_user", user.UserID, ErrInvalidUserID)
	}

	s.logger.Debug("Updating user", map[string]interface{}{
		"user_id": user.UserID,
		"rank":    user.Rank,
	})

	// Check if user exists
	_, err := s.GetUser(ctx, user.UserID)
	if err != nil {
		return err
	}

	// Verify that the rank exists if it's being changed
	if user.Rank != "" {
		_, err = s.GetRank(ctx, user.Rank)
		if err != nil {
			return NewUserAuthError("update_user", user.UserID, fmt.Errorf("invalid rank %s: %w", user.Rank, err))
		}
	}

	query := `
		UPDATE users 
		SET rank = ?, registered_by = ?, active = ?
		WHERE user_id = ? AND active = TRUE
	`

	result, err := s.db.ExecContext(ctx, query,
		user.Rank,
		user.RegisteredBy,
		user.Active,
		user.UserID,
	)
	if err != nil {
		s.logger.Error("Failed to update user", map[string]interface{}{
			"user_id": user.UserID,
			"rank":    user.Rank,
			"error":   err.Error(),
		})

		return NewUserAuthError("update_user", user.UserID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("Failed to get rows affected for user update", map[string]interface{}{
			"user_id": user.UserID,
			"error":   err.Error(),
		})

		return NewUserAuthError("update_user", user.UserID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	if rowsAffected == 0 {
		return NewUserAuthError("update_user", user.UserID, ErrUserNotFound)
	}

	s.logger.Info("User updated successfully", map[string]interface{}{
		"user_id": user.UserID,
		"rank":    user.Rank,
	})

	return nil
}

// DeleteUser soft-deletes a user by setting active to false.
func (s *SQLiteAuthStore) DeleteUser(ctx context.Context, userID string) error {
	if s.closed {
		return NewUserAuthError("delete_user", userID, ErrServiceClosed)
	}

	if userID == "" {
		return NewUserAuthError("delete_user", userID, ErrInvalidUserID)
	}

	s.logger.Debug("Deleting user", map[string]interface{}{
		"user_id": userID,
	})

	// Check if user exists first
	_, err := s.GetUser(ctx, userID)
	if err != nil {
		return err // This will return the appropriate error (not found, etc.)
	}

	query := `UPDATE users SET active = FALSE WHERE user_id = ?`

	result, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		s.logger.Error("Failed to delete user", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})

		return NewUserAuthError("delete_user", userID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("Failed to get rows affected for user deletion", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})

		return NewUserAuthError("delete_user", userID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	if rowsAffected == 0 {
		return NewUserAuthError("delete_user", userID, ErrUserNotFound)
	}

	s.logger.Info("User deleted successfully", map[string]interface{}{
		"user_id": userID,
	})

	return nil
}

// GetRank retrieves a rank by name.
func (s *SQLiteAuthStore) GetRank(ctx context.Context, rankName string) (*Rank, error) {
	if s.closed {
		return nil, NewAuthError("get_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rankName == "" {
		return nil, NewAuthError("get_rank", errors.New("rank name cannot be empty"))
	}

	s.logger.Debug("Getting rank", map[string]interface{}{
		"rank_name": rankName,
	})

	query := `
		SELECT name, level, commands, description, created_at, active 
		FROM ranks 
		WHERE name = ? AND active = TRUE
	`

	var rank Rank

	err := s.db.QueryRowContext(ctx, query, rankName).Scan(
		&rank.Name,
		&rank.Level,
		&rank.CommandsRaw,
		&rank.Description,
		&rank.CreatedAt,
		&rank.Active,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewAuthError("get_rank", fmt.Errorf("%w: rank %s", ErrRankNotFound, rankName))
	}

	if err != nil {
		s.logger.Error("Failed to get rank", map[string]interface{}{
			"rank_name": rankName,
			"error":     err.Error(),
		})

		return nil, NewAuthError("get_rank", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	// Unmarshal commands from JSON
	err = rank.UnmarshalCommands()
	if err != nil {
		s.logger.Error("Failed to unmarshal rank commands", map[string]interface{}{
			"rank_name": rankName,
			"error":     err.Error(),
		})

		return nil, NewAuthError("get_rank", fmt.Errorf("failed to parse rank commands: %w", err))
	}

	s.logger.Debug("Rank retrieved successfully", map[string]interface{}{
		"rank_name":     rankName,
		"command_count": len(rank.Commands),
	})

	return &rank, nil
}

// CreateRank creates a new rank.
func (s *SQLiteAuthStore) CreateRank(ctx context.Context, rank *Rank) error {
	if s.closed {
		return NewAuthError("create_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rank == nil {
		return NewAuthError("create_rank", errors.New("rank cannot be nil"))
	}

	if rank.Name == "" {
		return NewAuthError("create_rank", errors.New("rank name cannot be empty"))
	}

	s.logger.Debug("Creating rank", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	// Check if rank already exists
	existingRank, err := s.GetRank(ctx, rank.Name)
	if err != nil && !errors.Is(err, ErrRankNotFound) {
		// If it's not a "not found" error, it's a real database error
		var authErr *AuthError
		if errors.As(err, &authErr) && !errors.Is(authErr.Err, ErrRankNotFound) {
			return err
		}
	}

	if existingRank != nil {
		return NewAuthError("create_rank", fmt.Errorf("rank %s already exists", rank.Name))
	}

	// Marshal commands to JSON
	err = rank.MarshalCommands()
	if err != nil {
		return NewAuthError("create_rank", fmt.Errorf("failed to marshal commands: %w", err))
	}

	// Set defaults if not provided
	if rank.CreatedAt.IsZero() {
		rank.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO ranks (name, level, commands, description, created_at, active)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		rank.Name,
		rank.Level,
		rank.CommandsRaw,
		rank.Description,
		rank.CreatedAt,
		rank.Active,
	)
	if err != nil {
		s.logger.Error("Failed to create rank", map[string]interface{}{
			"rank_name": rank.Name,
			"error":     err.Error(),
		})

		return NewAuthError("create_rank", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Info("Rank created successfully", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	return nil
}

// UpdateRank updates an existing rank.
func (s *SQLiteAuthStore) UpdateRank(ctx context.Context, rank *Rank) error {
	if s.closed {
		return NewAuthError("update_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rank == nil {
		return NewAuthError("update_rank", errors.New("rank cannot be nil"))
	}

	if rank.Name == "" {
		return NewAuthError("update_rank", errors.New("rank name cannot be empty"))
	}

	s.logger.Debug("Updating rank", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	// Check if rank exists
	_, err := s.GetRank(ctx, rank.Name)
	if err != nil {
		return err
	}

	// Marshal commands to JSON
	err = rank.MarshalCommands()
	if err != nil {
		return NewAuthError("update_rank", fmt.Errorf("failed to marshal commands: %w", err))
	}

	query := `
		UPDATE ranks 
		SET level = ?, commands = ?, description = ?, active = ?
		WHERE name = ? AND active = TRUE
	`

	result, err := s.db.ExecContext(ctx, query,
		rank.Level,
		rank.CommandsRaw,
		rank.Description,
		rank.Active,
		rank.Name,
	)
	if err != nil {
		s.logger.Error("Failed to update rank", map[string]interface{}{
			"rank_name": rank.Name,
			"error":     err.Error(),
		})

		return NewAuthError("update_rank", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("Failed to get rows affected for rank update", map[string]interface{}{
			"rank_name": rank.Name,
			"error":     err.Error(),
		})

		return NewAuthError("update_rank", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	if rowsAffected == 0 {
		return NewAuthError("update_rank", fmt.Errorf("%w: rank %s", ErrRankNotFound, rank.Name))
	}

	s.logger.Info("Rank updated successfully", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	return nil
}

// ListRanks retrieves all active ranks.
func (s *SQLiteAuthStore) ListRanks(ctx context.Context) ([]*Rank, error) {
	if s.closed {
		return nil, NewAuthError("list_ranks", fmt.Errorf("%w", ErrServiceClosed))
	}

	s.logger.Debug("Listing all ranks", nil)

	query := `
		SELECT name, level, commands, description, created_at, active 
		FROM ranks 
		WHERE active = TRUE
		ORDER BY level ASC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		s.logger.Error("Failed to list ranks", map[string]interface{}{
			"error": err.Error(),
		})

		return nil, NewAuthError("list_ranks", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}
	defer rows.Close()

	var ranks []*Rank
	for rows.Next() {
		var rank Rank

		err = rows.Scan(
			&rank.Name,
			&rank.Level,
			&rank.CommandsRaw,
			&rank.Description,
			&rank.CreatedAt,
			&rank.Active,
		)
		if err != nil {
			s.logger.Error("Failed to scan rank", map[string]interface{}{
				"error": err.Error(),
			})

			return nil, NewAuthError("list_ranks", fmt.Errorf("%w: %w", ErrDatabaseError, err))
		}

		// Unmarshal commands from JSON
		err = rank.UnmarshalCommands()
		if err != nil {
			s.logger.Error("Failed to unmarshal rank commands", map[string]interface{}{
				"rank_name": rank.Name,
				"error":     err.Error(),
			})

			return nil, NewAuthError("list_ranks", fmt.Errorf("failed to parse rank commands for %s: %w", rank.Name, err))
		}

		ranks = append(ranks, &rank)
	}

	if err = rows.Err(); err != nil {
		s.logger.Error("Error iterating ranks", map[string]interface{}{
			"error": err.Error(),
		})

		return nil, NewAuthError("list_ranks", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Debug("Ranks listed successfully", map[string]interface{}{
		"rank_count": len(ranks),
	})

	return ranks, nil
}

// CountUsersWithRank counts the number of active users assigned to a specific rank.
func (s *SQLiteAuthStore) CountUsersWithRank(ctx context.Context, rankName string) (int, error) {
	if s.closed {
		return 0, NewAuthError("count_users_with_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rankName == "" {
		return 0, NewAuthError("count_users_with_rank", errors.New("rank name cannot be empty"))
	}

	s.logger.Debug("Counting users with rank", map[string]interface{}{
		"rank_name": rankName,
	})

	query := `SELECT COUNT(*) FROM users WHERE rank = ? AND active = TRUE`

	var count int

	err := s.db.QueryRowContext(ctx, query, rankName).Scan(&count)
	if err != nil {
		s.logger.Error("Failed to count users with rank", map[string]interface{}{
			"rank_name": rankName,
			"error":     err.Error(),
		})

		return 0, NewAuthError("count_users_with_rank", fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Debug("Users with rank counted", map[string]interface{}{
		"rank_name": rankName,
		"count":     count,
	})

	return count, nil
}

// GetGroup retrieves a registered group by ID.
func (s *SQLiteAuthStore) GetGroup(ctx context.Context, groupID string) (*RegisteredGroup, error) {
	if s.closed {
		return nil, NewGroupAuthError("get_group", groupID, ErrServiceClosed)
	}

	if groupID == "" {
		return nil, NewGroupAuthError("get_group", groupID, ErrInvalidGroupID)
	}

	s.logger.Debug("Getting group", map[string]interface{}{
		"group_id": groupID,
	})

	query := `
		SELECT group_id, registered_at, registered_by, active 
		FROM registered_groups 
		WHERE group_id = ? AND active = TRUE
	`

	var group RegisteredGroup

	err := s.db.QueryRowContext(ctx, query, groupID).Scan(
		&group.GroupID,
		&group.RegisteredAt,
		&group.RegisteredBy,
		&group.Active,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, NewGroupAuthError("get_group", groupID, ErrGroupNotRegistered)
	}

	if err != nil {
		s.logger.Error("Failed to get group", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})

		return nil, NewGroupAuthError("get_group", groupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Debug("Group retrieved successfully", map[string]interface{}{
		"group_id": groupID,
	})

	return &group, nil
}

// CreateGroup creates a new registered group.
func (s *SQLiteAuthStore) CreateGroup(ctx context.Context, group *RegisteredGroup) error {
	if s.closed {
		return NewGroupAuthError("create_group", group.GroupID, ErrServiceClosed)
	}

	if group == nil {
		return NewAuthError("create_group", errors.New("group cannot be nil"))
	}

	if group.GroupID == "" {
		return NewGroupAuthError("create_group", group.GroupID, ErrInvalidGroupID)
	}

	if group.RegisteredBy == "" {
		return NewGroupAuthError("create_group", group.GroupID, errors.New("registered_by cannot be empty"))
	}

	s.logger.Debug("Creating group", map[string]interface{}{
		"group_id":      group.GroupID,
		"registered_by": group.RegisteredBy,
	})

	// Check if group already exists
	existingGroup, err := s.GetGroup(ctx, group.GroupID)
	if err != nil && !errors.Is(err, ErrGroupNotRegistered) {
		// If it's not a "not found" error, it's a real database error
		var authErr *AuthError
		if errors.As(err, &authErr) && !errors.Is(authErr.Err, ErrGroupNotRegistered) {
			return err
		}
	}

	if existingGroup != nil {
		return NewGroupAuthError("create_group", group.GroupID, ErrGroupAlreadyRegistered)
	}

	// Verify that the registering user exists and is active
	_, err = s.GetUser(ctx, group.RegisteredBy)
	if err != nil {
		s.logger.Error("Cannot create group: registering user not found", map[string]interface{}{
			"group_id":      group.GroupID,
			"registered_by": group.RegisteredBy,
			"error":         err.Error(),
		})

		return NewGroupAuthError("create_group", group.GroupID, fmt.Errorf("registering user must be registered: %w", err))
	}

	// Set defaults if not provided
	if group.RegisteredAt.IsZero() {
		group.RegisteredAt = time.Now()
	}

	// Use transaction to ensure consistency
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Error("Failed to begin transaction for group creation", map[string]interface{}{
			"group_id": group.GroupID,
			"error":    err.Error(),
		})

		return NewGroupAuthError("create_group", group.GroupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}
	defer tx.Rollback()

	query := `
		INSERT INTO registered_groups (group_id, registered_at, registered_by, active)
		VALUES (?, ?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, query,
		group.GroupID,
		group.RegisteredAt,
		group.RegisteredBy,
		group.Active,
	)
	if err != nil {
		s.logger.Error("Failed to create group", map[string]interface{}{
			"group_id":      group.GroupID,
			"registered_by": group.RegisteredBy,
			"error":         err.Error(),
		})

		return NewGroupAuthError("create_group", group.GroupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	err = tx.Commit()
	if err != nil {
		s.logger.Error("Failed to commit group creation transaction", map[string]interface{}{
			"group_id": group.GroupID,
			"error":    err.Error(),
		})

		return NewGroupAuthError("create_group", group.GroupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	s.logger.Info("Group created successfully", map[string]interface{}{
		"group_id":      group.GroupID,
		"registered_by": group.RegisteredBy,
	})

	return nil
}

// DeleteGroup soft-deletes a registered group by setting active to false.
func (s *SQLiteAuthStore) DeleteGroup(ctx context.Context, groupID string) error {
	if s.closed {
		return NewGroupAuthError("delete_group", groupID, ErrServiceClosed)
	}

	if groupID == "" {
		return NewGroupAuthError("delete_group", groupID, ErrInvalidGroupID)
	}

	s.logger.Debug("Deleting group", map[string]interface{}{
		"group_id": groupID,
	})

	// Check if group exists first
	_, err := s.GetGroup(ctx, groupID)
	if err != nil {
		return err // This will return the appropriate error (not found, etc.)
	}

	query := `UPDATE registered_groups SET active = FALSE WHERE group_id = ?`

	result, err := s.db.ExecContext(ctx, query, groupID)
	if err != nil {
		s.logger.Error("Failed to delete group", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})

		return NewGroupAuthError("delete_group", groupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		s.logger.Error("Failed to get rows affected for group deletion", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})

		return NewGroupAuthError("delete_group", groupID, fmt.Errorf("%w: %w", ErrDatabaseError, err))
	}

	if rowsAffected == 0 {
		return NewGroupAuthError("delete_group", groupID, ErrGroupNotRegistered)
	}

	s.logger.Info("Group deleted successfully", map[string]interface{}{
		"group_id": groupID,
	})

	return nil
}

// IsWhatsAppAdmin checks if a user is a WhatsApp group admin.
func (s *SQLiteAuthStore) IsWhatsAppAdmin(ctx context.Context, userID, groupID string) (bool, error) {
	if s.closed {
		return false, NewUserAuthError("is_whatsapp_admin", userID, ErrServiceClosed)
	}

	if userID == "" {
		return false, NewUserAuthError("is_whatsapp_admin", userID, errors.New("user ID cannot be empty"))
	}

	if groupID == "" {
		return false, NewUserAuthError("is_whatsapp_admin", userID, errors.New("group ID cannot be empty"))
	}

	s.logger.Debug("Checking WhatsApp admin status", map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
	})

	// If WhatsApp client is not available, we cannot verify admin status
	if s.whatsappClient == nil {
		s.logger.Warn("WhatsApp client not available for admin verification", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
		})

		return false, NewUserAuthError("is_whatsapp_admin", userID, errors.New("WhatsApp client not available"))
	}

	// Check if the client is connected
	if !s.whatsappClient.IsConnected() {
		s.logger.Warn("WhatsApp client not connected for admin verification", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
		})

		return false, NewUserAuthError("is_whatsapp_admin", userID, errors.New("WhatsApp client not connected"))
	}

	// Use the WhatsApp client to check admin status
	isAdmin, err := s.whatsappClient.IsGroupAdmin(ctx, userID, groupID)
	if err != nil {
		s.logger.Error("Failed to check WhatsApp admin status", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
			"error":    err.Error(),
		})

		return false, NewUserAuthError("is_whatsapp_admin", userID, fmt.Errorf("failed to verify admin status: %w", err))
	}

	s.logger.Debug("WhatsApp admin status checked", map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
		"is_admin": isAdmin,
	})

	return isAdmin, nil
}

// Close closes the auth store and releases resources.
func (s *SQLiteAuthStore) Close() error {
	s.logger.Debug("Closing auth store", nil)
	s.closed = true
	s.logger.Info("Auth store closed", nil)

	return nil
}

// validateSchema ensures that the required database tables exist.
func (s *SQLiteAuthStore) validateSchema(ctx context.Context) error {
	// Check if users table exists (unified table)
	err := s.validateTable(ctx, "users", []string{
		"user_id", "rank", "registered_at", "registered_by", "active",
	})
	if err != nil {
		return fmt.Errorf("users table validation failed: %w", err)
	}

	// Check if ranks table exists
	err = s.validateTable(ctx, "ranks", []string{
		"name", "level", "commands", "description", "created_at", "active",
	})
	if err != nil {
		return fmt.Errorf("ranks table validation failed: %w", err)
	}

	// Check if registered_groups table exists
	err = s.validateTable(ctx, "registered_groups", []string{
		"group_id", "registered_at", "registered_by", "active",
	})
	if err != nil {
		return fmt.Errorf("registered_groups table validation failed: %w", err)
	}

	return nil
}

// validateTable checks if a table exists and has the expected columns.
func (s *SQLiteAuthStore) validateTable(ctx context.Context, tableName string, expectedColumns []string) error {
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name=?`

	var name string

	err := s.db.QueryRowContext(ctx, query, tableName).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("table %s does not exist", tableName)
	}

	if err != nil {
		return err
	}

	// Check columns
	pragmaQuery := fmt.Sprintf("PRAGMA table_info(%s)", tableName)

	rows, err := s.db.QueryContext(ctx, pragmaQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var actualColumns []string

	for rows.Next() {
		var (
			cid            int
			name, dataType string
			notNull, pk    int
			defaultValue   sql.NullString
		)

		err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			return err
		}

		actualColumns = append(actualColumns, name)
	}

	// Verify all expected columns exist
	for _, expected := range expectedColumns {
		found := false

		for _, actual := range actualColumns {
			if actual == expected {
				found = true

				break
			}
		}

		if !found {
			return fmt.Errorf("column %s not found in table %s", expected, tableName)
		}
	}

	return nil
}
