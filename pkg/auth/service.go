package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"botex/pkg/logger"
)

// UnifiedAuthService implements the unified AuthService interface
type UnifiedAuthService struct {
	store  AuthStore
	logger *logger.Logger
	closed bool
}

// NewUnifiedAuthService creates a new unified auth service instance
func NewUnifiedAuthService(store AuthStore, loggerFactory *logger.Factory) (*UnifiedAuthService, error) {
	if store == nil {
		return nil, NewAuthError("service_init", fmt.Errorf("auth store cannot be nil"))
	}

	if loggerFactory == nil {
		return nil, NewAuthError("service_init", fmt.Errorf("logger factory cannot be nil"))
	}

	serviceLogger := loggerFactory.GetLogger("unified-auth-service")

	service := &UnifiedAuthService{
		store:  store,
		logger: serviceLogger,
		closed: false,
	}

	serviceLogger.Info("Unified auth service initialized successfully", nil)
	return service, nil
}

// CheckPermission is the primary method for unified permission checking with early return
func (s *UnifiedAuthService) CheckPermission(ctx context.Context, userID, groupID, command string) (*PermissionResult, error) {
	// Initialize performance metrics
	metrics := NewPerformanceMetrics("check_permission", userID, groupID, command)
	
	// Log concurrent operation safety - this operation is concurrent-safe
	s.logConcurrentOperationSafety("check_permission", true, map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
		"command":  command,
	})
	
	if s.closed {
		err := NewUserGroupAuthError("check_permission", userID, groupID, ErrServiceClosed)
		metrics.Complete(false, err.Error())
		s.logOperationError("check_permission", metrics, err, nil)
		return nil, err
	}

	if userID == "" {
		err := NewUserGroupAuthError("check_permission", userID, groupID, ErrInvalidUserID)
		metrics.Complete(false, err.Error())
		s.logOperationError("check_permission", metrics, err, nil)
		return nil, err
	}

	if command == "" {
		err := NewUserGroupAuthError("check_permission", userID, groupID, fmt.Errorf("command cannot be empty"))
		metrics.Complete(false, err.Error())
		s.logOperationError("check_permission", metrics, err, nil)
		return nil, err
	}

	s.logOperationStart("check_permission", map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
		"command":  command,
	})

	result := &PermissionResult{
		Allowed:         false,
		Reason:          "",
		UserRank:        "",
		IsWhatsAppAdmin: false,
	}

	// Determine context: private chat vs group chat
	isPrivateChat := groupID == ""

	// Early return: Check group registration first for group chats (fastest check)
	if !isPrivateChat {
		isGroupRegistered, err := s.IsGroupRegistered(ctx, groupID)
		if err != nil {
			metrics.Complete(false, err.Error())
			s.logOperationError("check_permission", metrics, err, map[string]interface{}{
				"step": "group_registration_check",
			})
			return nil, err
		}

		if !isGroupRegistered {
			result.Reason = "Group not registered"
			metrics.Complete(true, "")
			s.logOperationSuccess("check_permission", metrics, map[string]interface{}{
				"result":  "denied",
				"reason":  result.Reason,
				"step":    "group_registration_check",
			})
			return result, nil
		}
	}

	// Get user information (includes rank)
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) && errors.Is(authErr.Err, ErrUserNotFound) {
			// User not registered
			if isPrivateChat {
				result.Reason = "User not registered for private chat access"
			} else {
				result.Reason = "User not registered"
			}
			metrics.Complete(true, "")
			s.logOperationSuccess("check_permission", metrics, map[string]interface{}{
				"result": "denied",
				"reason": result.Reason,
				"step":   "user_lookup",
			})
			return result, nil
		}
		// Other database errors
		metrics.Complete(false, err.Error())
		s.logOperationError("check_permission", metrics, err, map[string]interface{}{
			"step": "user_lookup",
		})
		return nil, err
	}

	result.UserRank = user.Rank

	// Get user's rank permissions
	rank, err := s.store.GetRank(ctx, user.Rank)
	if err != nil {
		result.Reason = "Unable to determine user permissions"
		metrics.Complete(false, err.Error())
		s.logOperationError("check_permission", metrics, err, map[string]interface{}{
			"step":      "rank_lookup",
			"user_rank": user.Rank,
		})
		return result, nil
	}

	// Check if user's rank has permission for this command
	if !rank.HasCommand(command) {
		result.Reason = fmt.Sprintf("Command '%s' not allowed for rank '%s'", command, user.Rank)
		metrics.Complete(true, "")
		s.logOperationSuccess("check_permission", metrics, map[string]interface{}{
			"result":    "denied",
			"reason":    result.Reason,
			"step":      "command_permission_check",
			"user_rank": user.Rank,
		})
		return result, nil
	}

	// Check WhatsApp admin status for group context
	if !isPrivateChat {
		isAdmin, err := s.store.IsWhatsAppAdmin(ctx, userID, groupID)
		if err != nil {
			s.logger.Warn("Failed to check WhatsApp admin status", map[string]interface{}{
				"user_id":  userID,
				"group_id": groupID,
				"error":    err.Error(),
			})
			// Don't fail the permission check, just log the warning
		} else {
			result.IsWhatsAppAdmin = isAdmin
		}
	}

	// Permission granted
	result.Allowed = true
	result.Reason = "Access granted"

	// Complete performance metrics and log success
	metrics.Complete(true, "")
	s.logOperationSuccess("check_permission", metrics, map[string]interface{}{
		"result":            "allowed",
		"user_rank":         result.UserRank,
		"is_whatsapp_admin": result.IsWhatsAppAdmin,
		"step":              "completed",
	})

	return result, nil
}

// IsUserRegistered checks if a user is registered in the system
func (s *UnifiedAuthService) IsUserRegistered(ctx context.Context, userID string) (bool, error) {
	if s.closed {
		return false, NewUserAuthError("is_user_registered", userID, ErrServiceClosed)
	}

	if userID == "" {
		return false, NewUserAuthError("is_user_registered", userID, ErrInvalidUserID)
	}

	s.logger.Debug("Checking user registration status", map[string]interface{}{
		"user_id": userID,
	})

	_, err := s.store.GetUser(ctx, userID)
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) && errors.Is(authErr.Err, ErrUserNotFound) {
			s.logger.Debug("User not registered", map[string]interface{}{
				"user_id": userID,
			})
			return false, nil
		}
		// Return other errors as-is
		return false, err
	}

	s.logger.Debug("User is registered", map[string]interface{}{
		"user_id": userID,
	})
	return true, nil
}

// RegisterUser registers a new user in the system with specified rank
func (s *UnifiedAuthService) RegisterUser(ctx context.Context, userID, rank string) error {
	// Initialize performance metrics
	metrics := NewPerformanceMetrics("register_user", userID, "", "")
	
	// Log concurrent operation safety - this operation is concurrent-safe
	s.logConcurrentOperationSafety("register_user", true, map[string]interface{}{
		"user_id": userID,
		"rank":    rank,
	})
	
	if s.closed {
		err := NewUserAuthError("register_user", userID, ErrServiceClosed)
		metrics.Complete(false, err.Error())
		s.logOperationError("register_user", metrics, err, nil)
		return err
	}

	if userID == "" {
		err := NewUserAuthError("register_user", userID, ErrInvalidUserID)
		metrics.Complete(false, err.Error())
		s.logOperationError("register_user", metrics, err, nil)
		return err
	}

	if rank == "" {
		rank = "basic" // Default rank
	}

	// Validate userID format (basic validation)
	if strings.TrimSpace(userID) != userID {
		err := NewUserAuthError("register_user", userID, fmt.Errorf("user ID cannot have leading/trailing whitespace"))
		metrics.Complete(false, err.Error())
		s.logOperationError("register_user", metrics, err, nil)
		return err
	}

	s.logOperationStart("register_user", map[string]interface{}{
		"user_id": userID,
		"rank":    rank,
	})

	// Check if user is already registered
	isRegistered, err := s.IsUserRegistered(ctx, userID)
	if err != nil {
		return err
	}

	if isRegistered {
		err := NewUserAuthError("register_user", userID, ErrUserAlreadyExists)
		metrics.Complete(false, err.Error())
		s.logOperationError("register_user", metrics, err, map[string]interface{}{
			"step": "duplicate_check",
		})
		return err
	}

	// Create new user record with unified model
	user := &User{
		UserID:       userID,
		Rank:         rank,
		RegisteredAt: time.Now(),
		RegisteredBy: nil, // API registration, no specific user
		Active:       true,
	}

	err = s.store.CreateUser(ctx, user)
	if err != nil {
		metrics.Complete(false, err.Error())
		s.logOperationError("register_user", metrics, err, map[string]interface{}{
			"step": "database_create",
			"rank": rank,
		})
		return err
	}

	metrics.Complete(true, "")
	s.logOperationSuccess("register_user", metrics, map[string]interface{}{
		"rank": rank,
		"step": "completed",
	})

	return nil
}

// UnregisterUser removes a user from the system
func (s *UnifiedAuthService) UnregisterUser(ctx context.Context, userID string) error {
	if s.closed {
		return NewUserAuthError("unregister_user", userID, ErrServiceClosed)
	}

	if userID == "" {
		return NewUserAuthError("unregister_user", userID, ErrInvalidUserID)
	}

	s.logger.Info("Unregistering user", map[string]interface{}{
		"user_id": userID,
	})

	// Check if user is registered
	isRegistered, err := s.IsUserRegistered(ctx, userID)
	if err != nil {
		return err
	}

	if !isRegistered {
		s.logger.Warn("Attempted to unregister non-registered user", map[string]interface{}{
			"user_id": userID,
		})
		return NewUserAuthError("unregister_user", userID, ErrUserNotFound)
	}

	err = s.store.DeleteUser(ctx, userID)
	if err != nil {
		s.logger.Error("Failed to unregister user", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		return err
	}

	s.logger.Info("User unregistered successfully", map[string]interface{}{
		"user_id": userID,
	})

	return nil
}

// GetUser retrieves a user by ID
func (s *UnifiedAuthService) GetUser(ctx context.Context, userID string) (*User, error) {
	if s.closed {
		return nil, NewUserAuthError("get_user", userID, ErrServiceClosed)
	}

	if userID == "" {
		return nil, NewUserAuthError("get_user", userID, ErrInvalidUserID)
	}

	s.logger.Debug("Getting user", map[string]interface{}{
		"user_id": userID,
	})

	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		s.logger.Debug("Failed to get user", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		return nil, err
	}

	s.logger.Debug("User retrieved successfully", map[string]interface{}{
		"user_id": userID,
		"rank":    user.Rank,
	})

	return user, nil
}

// SetUserRank assigns or updates a user's rank with validation
func (s *UnifiedAuthService) SetUserRank(ctx context.Context, userID, rank string) error {
	if s.closed {
		return NewUserAuthError("set_user_rank", userID, ErrServiceClosed)
	}

	if userID == "" {
		return NewUserAuthError("set_user_rank", userID, ErrInvalidUserID)
	}

	if rank == "" {
		return NewUserAuthError("set_user_rank", userID, fmt.Errorf("rank cannot be empty"))
	}

	// Validate that the rank exists and is active
	_, err := s.store.GetRank(ctx, rank)
	if err != nil {
		return NewUserAuthError("set_user_rank", userID, fmt.Errorf("invalid rank %s: %w", rank, err))
	}

	s.logger.Info("Setting user rank", map[string]interface{}{
		"user_id": userID,
		"rank":    rank,
	})

	// Get existing user
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	// Update rank
	user.Rank = rank

	err = s.store.UpdateUser(ctx, user)
	if err != nil {
		s.logger.Error("Failed to set user rank", map[string]interface{}{
			"user_id": userID,
			"rank":    rank,
			"error":   err.Error(),
		})
		return err
	}

	s.logger.Info("User rank updated successfully", map[string]interface{}{
		"user_id": userID,
		"rank":    rank,
	})

	return nil
}

// IsGroupRegistered checks if a group is registered in the system
func (s *UnifiedAuthService) IsGroupRegistered(ctx context.Context, groupID string) (bool, error) {
	if s.closed {
		return false, NewGroupAuthError("is_group_registered", groupID, ErrServiceClosed)
	}

	if groupID == "" {
		return false, NewGroupAuthError("is_group_registered", groupID, ErrInvalidGroupID)
	}

	s.logger.Debug("Checking group registration status", map[string]interface{}{
		"group_id": groupID,
	})

	_, err := s.store.GetGroup(ctx, groupID)
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) && errors.Is(authErr.Err, ErrGroupNotRegistered) {
			s.logger.Debug("Group not registered", map[string]interface{}{
				"group_id": groupID,
			})
			return false, nil
		}
		// Return other errors as-is
		return false, err
	}

	s.logger.Debug("Group is registered", map[string]interface{}{
		"group_id": groupID,
	})
	return true, nil
}

// RegisterGroup registers a new group in the system
func (s *UnifiedAuthService) RegisterGroup(ctx context.Context, groupID, registeredBy string) error {
	if s.closed {
		return NewGroupAuthError("register_group", groupID, ErrServiceClosed)
	}

	if groupID == "" {
		return NewGroupAuthError("register_group", groupID, ErrInvalidGroupID)
	}

	if registeredBy == "" {
		return NewGroupAuthError("register_group", groupID, fmt.Errorf("registeredBy cannot be empty"))
	}

	// Validate IDs format (basic validation)
	if strings.TrimSpace(groupID) != groupID {
		return NewGroupAuthError("register_group", groupID, fmt.Errorf("group ID cannot have leading/trailing whitespace"))
	}

	if strings.TrimSpace(registeredBy) != registeredBy {
		return NewGroupAuthError("register_group", groupID, fmt.Errorf("registeredBy cannot have leading/trailing whitespace"))
	}

	s.logger.Info("Registering group", map[string]interface{}{
		"group_id":      groupID,
		"registered_by": registeredBy,
	})

	// Check if the registering user is registered
	isUserRegistered, err := s.IsUserRegistered(ctx, registeredBy)
	if err != nil {
		return NewGroupAuthError("register_group", groupID, fmt.Errorf("failed to verify registering user: %w", err))
	}

	if !isUserRegistered {
		s.logger.Warn("Attempted group registration by unregistered user", map[string]interface{}{
			"group_id":      groupID,
			"registered_by": registeredBy,
		})
		return NewGroupAuthError("register_group", groupID, fmt.Errorf("registering user must be registered: %w", ErrUserNotFound))
	}

	// Check if group is already registered
	isRegistered, err := s.IsGroupRegistered(ctx, groupID)
	if err != nil {
		return err
	}

	if isRegistered {
		s.logger.Warn("Attempted to register already registered group", map[string]interface{}{
			"group_id":      groupID,
			"registered_by": registeredBy,
		})
		return NewGroupAuthError("register_group", groupID, ErrGroupAlreadyRegistered)
	}

	// Create new group record
	group := &RegisteredGroup{
		GroupID:      groupID,
		RegisteredAt: time.Now(),
		RegisteredBy: registeredBy,
		Active:       true,
	}

	err = s.store.CreateGroup(ctx, group)
	if err != nil {
		s.logger.Error("Failed to register group", map[string]interface{}{
			"group_id":      groupID,
			"registered_by": registeredBy,
			"error":         err.Error(),
		})
		return err
	}

	s.logger.Info("Group registered successfully", map[string]interface{}{
		"group_id":      groupID,
		"registered_by": registeredBy,
	})

	return nil
}

// UnregisterGroup removes a group from the system
func (s *UnifiedAuthService) UnregisterGroup(ctx context.Context, groupID string) error {
	if s.closed {
		return NewGroupAuthError("unregister_group", groupID, ErrServiceClosed)
	}

	if groupID == "" {
		return NewGroupAuthError("unregister_group", groupID, ErrInvalidGroupID)
	}

	s.logger.Info("Unregistering group", map[string]interface{}{
		"group_id": groupID,
	})

	// Check if group is registered
	isRegistered, err := s.IsGroupRegistered(ctx, groupID)
	if err != nil {
		return err
	}

	if !isRegistered {
		s.logger.Warn("Attempted to unregister non-registered group", map[string]interface{}{
			"group_id": groupID,
		})
		return NewGroupAuthError("unregister_group", groupID, ErrGroupNotRegistered)
	}

	err = s.store.DeleteGroup(ctx, groupID)
	if err != nil {
		s.logger.Error("Failed to unregister group", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})
		return err
	}

	s.logger.Info("Group unregistered successfully", map[string]interface{}{
		"group_id": groupID,
	})

	return nil
}

// GetRank retrieves a rank by name (for future API)
func (s *UnifiedAuthService) GetRank(ctx context.Context, rankName string) (*Rank, error) {
	if s.closed {
		return nil, NewAuthError("get_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rankName == "" {
		return nil, NewAuthError("get_rank", fmt.Errorf("rank name cannot be empty"))
	}

	s.logger.Debug("Getting rank", map[string]interface{}{
		"rank_name": rankName,
	})

	rank, err := s.store.GetRank(ctx, rankName)
	if err != nil {
		s.logger.Debug("Failed to get rank", map[string]interface{}{
			"rank_name": rankName,
			"error":     err.Error(),
		})
		return nil, err
	}

	s.logger.Debug("Rank retrieved successfully", map[string]interface{}{
		"rank_name":     rankName,
		"command_count": len(rank.Commands),
	})

	return rank, nil
}

// CreateRank creates a new rank with hierarchy validation (for future API)
func (s *UnifiedAuthService) CreateRank(ctx context.Context, rank *Rank) error {
	if s.closed {
		return NewAuthError("create_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rank == nil {
		return NewAuthError("create_rank", fmt.Errorf("rank cannot be nil"))
	}

	// Validate rank hierarchy
	err := s.validateRankHierarchy(ctx, rank)
	if err != nil {
		return NewAuthError("create_rank", fmt.Errorf("rank hierarchy validation failed: %w", err))
	}

	s.logger.Info("Creating rank", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	err = s.store.CreateRank(ctx, rank)
	if err != nil {
		s.logger.Error("Failed to create rank", map[string]interface{}{
			"rank_name": rank.Name,
			"error":     err.Error(),
		})
		return err
	}

	s.logger.Info("Rank created successfully", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	return nil
}

// UpdateRank updates an existing rank with hierarchy validation (for future API)
func (s *UnifiedAuthService) UpdateRank(ctx context.Context, rank *Rank) error {
	if s.closed {
		return NewAuthError("update_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rank == nil {
		return NewAuthError("update_rank", fmt.Errorf("rank cannot be nil"))
	}

	// Validate rank hierarchy
	err := s.validateRankHierarchy(ctx, rank)
	if err != nil {
		return NewAuthError("update_rank", fmt.Errorf("rank hierarchy validation failed: %w", err))
	}

	s.logger.Info("Updating rank", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	err = s.store.UpdateRank(ctx, rank)
	if err != nil {
		s.logger.Error("Failed to update rank", map[string]interface{}{
			"rank_name": rank.Name,
			"error":     err.Error(),
		})
		return err
	}

	s.logger.Info("Rank updated successfully", map[string]interface{}{
		"rank_name": rank.Name,
		"level":     rank.Level,
	})

	return nil
}

// ListRanks retrieves all active ranks ordered by hierarchy level
func (s *UnifiedAuthService) ListRanks(ctx context.Context) ([]*Rank, error) {
	if s.closed {
		return nil, NewAuthError("list_ranks", fmt.Errorf("%w", ErrServiceClosed))
	}

	s.logger.Debug("Listing all ranks", nil)

	ranks, err := s.store.ListRanks(ctx)
	if err != nil {
		s.logger.Error("Failed to list ranks", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, err
	}

	s.logger.Debug("Ranks listed successfully", map[string]interface{}{
		"rank_count": len(ranks),
	})

	return ranks, nil
}

// DeleteRank soft-deletes a rank by setting active to false
func (s *UnifiedAuthService) DeleteRank(ctx context.Context, rankName string) error {
	if s.closed {
		return NewAuthError("delete_rank", fmt.Errorf("%w", ErrServiceClosed))
	}

	if rankName == "" {
		return NewAuthError("delete_rank", fmt.Errorf("rank name cannot be empty"))
	}

	// Prevent deletion of default ranks
	if s.isDefaultRank(rankName) {
		return NewAuthError("delete_rank", fmt.Errorf("cannot delete default rank: %s", rankName))
	}

	s.logger.Info("Deleting rank", map[string]interface{}{
		"rank_name": rankName,
	})

	// Check if any users are assigned to this rank
	hasUsers, err := s.rankHasUsers(ctx, rankName)
	if err != nil {
		return NewAuthError("delete_rank", fmt.Errorf("failed to check rank usage: %w", err))
	}

	if hasUsers {
		return NewAuthError("delete_rank", fmt.Errorf("cannot delete rank %s: users are assigned to this rank", rankName))
	}

	// Get the rank to update it
	rank, err := s.store.GetRank(ctx, rankName)
	if err != nil {
		return err
	}

	// Set rank as inactive
	rank.Active = false

	err = s.store.UpdateRank(ctx, rank)
	if err != nil {
		s.logger.Error("Failed to delete rank", map[string]interface{}{
			"rank_name": rankName,
			"error":     err.Error(),
		})
		return err
	}

	s.logger.Info("Rank deleted successfully", map[string]interface{}{
		"rank_name": rankName,
	})

	return nil
}

// InitializeDefaultRanks creates the default owner and basic ranks if they don't exist
func (s *UnifiedAuthService) InitializeDefaultRanks(ctx context.Context) error {
	if s.closed {
		return NewAuthError("initialize_default_ranks", fmt.Errorf("%w", ErrServiceClosed))
	}

	s.logger.Info("Initializing default ranks", nil)

	for _, defaultRank := range DefaultRanks {
		// Check if rank already exists
		_, err := s.store.GetRank(ctx, defaultRank.Name)
		if err == nil {
			// Rank exists, skip
			s.logger.Debug("Default rank already exists", map[string]interface{}{
				"rank_name": defaultRank.Name,
			})
			continue
		}

		// Check if it's a "not found" error
		var authErr *AuthError
		if !errors.As(err, &authErr) || !errors.Is(authErr.Err, ErrRankNotFound) {
			// It's a real error, not just "not found"
			return NewAuthError("initialize_default_ranks", fmt.Errorf("failed to check existing rank %s: %w", defaultRank.Name, err))
		}

		// Create the default rank
		err = s.store.CreateRank(ctx, defaultRank)
		if err != nil {
			s.logger.Error("Failed to create default rank", map[string]interface{}{
				"rank_name": defaultRank.Name,
				"error":     err.Error(),
			})
			return NewAuthError("initialize_default_ranks", fmt.Errorf("failed to create default rank %s: %w", defaultRank.Name, err))
		}

		s.logger.Info("Default rank created", map[string]interface{}{
			"rank_name": defaultRank.Name,
			"level":     defaultRank.Level,
		})
	}

	s.logger.Info("Default ranks initialization completed", nil)
	return nil
}

// validateRankHierarchy validates that the rank hierarchy is consistent
func (s *UnifiedAuthService) validateRankHierarchy(ctx context.Context, rank *Rank) error {
	if rank.Name == "" {
		return fmt.Errorf("rank name cannot be empty")
	}

	if rank.Level < 0 {
		return fmt.Errorf("rank level cannot be negative")
	}

	// Validate commands are not nil
	if rank.Commands == nil {
		rank.Commands = []string{}
	}

	// Check for level conflicts with existing ranks (excluding the rank being updated)
	existingRanks, err := s.store.ListRanks(ctx)
	if err != nil {
		return fmt.Errorf("failed to get existing ranks: %w", err)
	}

	for _, existingRank := range existingRanks {
		// Skip the rank being updated
		if existingRank.Name == rank.Name {
			continue
		}

		// Check for level conflicts
		if existingRank.Level == rank.Level {
			return fmt.Errorf("rank level %d is already used by rank '%s'", rank.Level, existingRank.Name)
		}
	}

	// Validate hierarchy rules
	err = s.validateHierarchyRules(rank, existingRanks)
	if err != nil {
		return fmt.Errorf("hierarchy validation failed: %w", err)
	}

	return nil
}

// validateHierarchyRules validates specific hierarchy rules
func (s *UnifiedAuthService) validateHierarchyRules(rank *Rank, existingRanks []*Rank) error {
	// Rule: Owner rank should have level 0 (highest privilege)
	if rank.Name == "owner" && rank.Level != 0 {
		return fmt.Errorf("owner rank must have level 0")
	}

	// Rule: Basic rank should have the highest level number (lowest privilege)
	if rank.Name == "basic" {
		maxLevel := rank.Level
		for _, existingRank := range existingRanks {
			if existingRank.Name != "basic" && existingRank.Level > maxLevel {
				maxLevel = existingRank.Level
			}
		}
		if rank.Level < maxLevel {
			return fmt.Errorf("basic rank should have the highest level number (currently %d, but found level %d)", rank.Level, maxLevel)
		}
	}

	// Rule: No rank should have level 0 except owner
	if rank.Name != "owner" && rank.Level == 0 {
		return fmt.Errorf("only owner rank can have level 0")
	}

	return nil
}

// isDefaultRank checks if a rank is one of the default ranks
func (s *UnifiedAuthService) isDefaultRank(rankName string) bool {
	for _, defaultRank := range DefaultRanks {
		if defaultRank.Name == rankName {
			return true
		}
	}
	return false
}

// rankHasUsers checks if any users are assigned to the specified rank
func (s *UnifiedAuthService) rankHasUsers(ctx context.Context, rankName string) (bool, error) {
	s.logger.Debug("Checking if rank has users", map[string]interface{}{
		"rank_name": rankName,
	})
	
	count, err := s.store.CountUsersWithRank(ctx, rankName)
	if err != nil {
		return false, fmt.Errorf("failed to count users with rank: %w", err)
	}
	
	return count > 0, nil
}

// logOperationStart logs the start of an operation with context
func (s *UnifiedAuthService) logOperationStart(operation string, data map[string]interface{}) {
	logData := map[string]interface{}{
		"operation": operation,
		"status":    "started",
	}
	for k, v := range data {
		logData[k] = v
	}
	s.logger.Debug("Operation started", logData)
}

// logOperationSuccess logs successful completion of an operation with performance metrics
func (s *UnifiedAuthService) logOperationSuccess(operation string, metrics *PerformanceMetrics, additionalData map[string]interface{}) {
	logData := metrics.ToLogData()
	logData["status"] = "success"
	
	for k, v := range additionalData {
		logData[k] = v
	}
	
	if metrics.IsSlowOperation(50 * time.Millisecond) {
		s.logger.Warn("Slow operation completed successfully", logData)
	} else {
		s.logger.Debug("Operation completed successfully", logData)
	}
}

// logOperationError logs operation failure with performance metrics and error context
func (s *UnifiedAuthService) logOperationError(operation string, metrics *PerformanceMetrics, err error, additionalData map[string]interface{}) {
	logData := metrics.ToLogData()
	logData["status"] = "error"
	logData["error"] = err.Error()
	
	for k, v := range additionalData {
		logData[k] = v
	}
	
	// Determine log level based on error type
	var authErr *AuthError
	if errors.As(err, &authErr) {
		// Check if it's a user error (not found, invalid input) vs system error
		if errors.Is(authErr.Err, ErrUserNotFound) || 
		   errors.Is(authErr.Err, ErrGroupNotRegistered) ||
		   errors.Is(authErr.Err, ErrInvalidUserID) ||
		   errors.Is(authErr.Err, ErrInvalidGroupID) {
			s.logger.Debug("Operation failed with user error", logData)
		} else {
			s.logger.Error("Operation failed with system error", logData)
		}
	} else {
		s.logger.Error("Operation failed with unexpected error", logData)
	}
}

// logConcurrentOperationSafety logs information about concurrent operation safety
func (s *UnifiedAuthService) logConcurrentOperationSafety(operation string, concurrent bool, data map[string]interface{}) {
	logData := map[string]interface{}{
		"operation":           operation,
		"concurrent_safe":     concurrent,
		"blocking_operations": !concurrent,
	}
	for k, v := range data {
		logData[k] = v
	}
	
	if !concurrent {
		s.logger.Warn("Non-concurrent operation detected", logData)
	} else {
		s.logger.Debug("Concurrent-safe operation", logData)
	}
}

// Close closes the unified auth service and releases resources
func (s *UnifiedAuthService) Close() error {
	if s.closed {
		return nil
	}

	s.logger.Info("Closing unified auth service", nil)

	// Close the store if it implements Close
	if s.store != nil {
		err := s.store.Close()
		if err != nil {
			s.logger.Error("Failed to close auth store", map[string]interface{}{
				"error": err.Error(),
			})
			return NewAuthError("service_close", fmt.Errorf("failed to close auth store: %w", err))
		}
	}

	s.closed = true
	s.logger.Info("Unified auth service closed successfully", nil)
	return nil
}