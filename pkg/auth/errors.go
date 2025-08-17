package auth

import (
	"errors"
	"fmt"
	"time"
)

// Core auth errors.
var (
	ErrUserNotRegistered       = errors.New("user not registered")
	ErrUserNotFound            = errors.New("user not found")
	ErrUserAlreadyExists       = errors.New("user already exists")
	ErrGroupNotRegistered      = errors.New("group not registered")
	ErrGroupAlreadyRegistered  = errors.New("group already registered")
	ErrRankNotFound            = errors.New("rank not found")
	ErrInsufficientPermissions = errors.New("insufficient permissions")
	ErrInsufficientRank        = errors.New("insufficient rank for command")
	ErrInvalidCommand          = errors.New("command not allowed")
	ErrDatabaseError           = errors.New("database error")
	ErrInvalidUserID           = errors.New("invalid user ID")
	ErrInvalidGroupID          = errors.New("invalid group ID")
	ErrServiceClosed           = errors.New("auth service is closed")
)

// AuthError wraps auth-specific errors with additional context.
type AuthError struct {
	Op      string // operation that failed
	UserID  string // user ID if relevant
	GroupID string // group ID if relevant
	Err     error  // underlying error
}

func (e *AuthError) Error() string {
	if e.UserID != "" && e.GroupID != "" {
		return fmt.Sprintf("auth %s failed for user %s in group %s: %v", e.Op, e.UserID, e.GroupID, e.Err)
	}

	if e.UserID != "" {
		return fmt.Sprintf("auth %s failed for user %s: %v", e.Op, e.UserID, e.Err)
	}

	if e.GroupID != "" {
		return fmt.Sprintf("auth %s failed for group %s: %v", e.Op, e.GroupID, e.Err)
	}

	return fmt.Sprintf("auth %s failed: %v", e.Op, e.Err)
}

func (e *AuthError) Unwrap() error {
	return e.Err
}

// NewAuthError creates a new AuthError with the given operation and underlying error.
func NewAuthError(op string, err error) *AuthError {
	return &AuthError{
		Op:  op,
		Err: err,
	}
}

// NewUserAuthError creates a new AuthError for user-specific operations.
func NewUserAuthError(op, userID string, err error) *AuthError {
	return &AuthError{
		Op:     op,
		UserID: userID,
		Err:    err,
	}
}

// NewGroupAuthError creates a new AuthError for group-specific operations.
func NewGroupAuthError(op, groupID string, err error) *AuthError {
	return &AuthError{
		Op:      op,
		GroupID: groupID,
		Err:     err,
	}
}

// NewUserGroupAuthError creates a new AuthError for operations involving both user and group.
func NewUserGroupAuthError(op, userID, groupID string, err error) *AuthError {
	return &AuthError{
		Op:      op,
		UserID:  userID,
		GroupID: groupID,
		Err:     err,
	}
}

// PerformanceMetrics holds timing and performance data for operations.
type PerformanceMetrics struct {
	Operation    string
	StartTime    time.Time
	Duration     time.Duration
	UserID       string
	GroupID      string
	Command      string
	Success      bool
	ErrorMessage string
}

// NewPerformanceMetrics creates a new performance metrics tracker.
func NewPerformanceMetrics(operation, userID, groupID, command string) *PerformanceMetrics {
	return &PerformanceMetrics{
		Operation: operation,
		StartTime: time.Now(),
		UserID:    userID,
		GroupID:   groupID,
		Command:   command,
		Success:   false,
	}
}

// Complete marks the operation as complete and calculates duration.
func (pm *PerformanceMetrics) Complete(success bool, errorMessage string) {
	pm.Duration = time.Since(pm.StartTime)
	pm.Success = success
	pm.ErrorMessage = errorMessage
}

// ToLogData converts performance metrics to structured log data.
func (pm *PerformanceMetrics) ToLogData() map[string]interface{} {
	data := map[string]interface{}{
		"operation":   pm.Operation,
		"duration_ms": pm.Duration.Milliseconds(),
		"duration_ns": pm.Duration.Nanoseconds(),
		"success":     pm.Success,
	}

	if pm.UserID != "" {
		data["user_id"] = pm.UserID
	}

	if pm.GroupID != "" {
		data["group_id"] = pm.GroupID
	}

	if pm.Command != "" {
		data["command"] = pm.Command
	}

	if pm.ErrorMessage != "" {
		data["error"] = pm.ErrorMessage
	}

	return data
}

// IsSlowOperation returns true if the operation took longer than the threshold.
func (pm *PerformanceMetrics) IsSlowOperation(threshold time.Duration) bool {
	return pm.Duration > threshold
}
