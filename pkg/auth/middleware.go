package auth

import (
	"context"
	"fmt"
)

// Middleware provides authentication and authorization middleware functions.
type Middleware struct {
	service *Service
}

// NewMiddleware creates a new middleware instance.
func NewMiddleware(service *Service) *Middleware {
	return &Middleware{service: service}
}

// RequirePermission creates a middleware that checks if a user has a specific permission.
func (m *Middleware) RequirePermission(permissionName string) func(ctx context.Context, jid string) error {
	return func(ctx context.Context, jid string) error {
		hasPermission, err := m.service.HasPermission(ctx, jid, permissionName)
		if err != nil {
			return fmt.Errorf("error checking permission: %w", err)
		}
		if !hasPermission {
			return ErrAccessDenied
		}

		return nil
	}
}

// RequireResourceAccess creates a middleware that checks if a user has sufficient resource access.
func (m *Middleware) RequireResourceAccess(permissionName string, resourceCost int) func(ctx context.Context, jid string) error {
	return func(ctx context.Context, jid string) error {
		hasAccess, err := m.service.CheckResourceAccess(ctx, jid, permissionName, resourceCost)
		if err != nil {
			return fmt.Errorf("error checking resource access: %w", err)
		}
		if !hasAccess {
			return ErrAccessDenied
		}

		return nil
	}
}

// RequireRank creates a middleware that checks if a user has a specific rank.
func (m *Middleware) RequireRank(rankName string) func(ctx context.Context, jid string) error {
	return func(ctx context.Context, jid string) error {
		rank, err := m.service.GetUserRank(ctx, jid)
		if err != nil {
			return fmt.Errorf("error getting user rank: %w", err)
		}
		if rank.Name != rankName {
			return ErrAccessDenied
		}

		return nil
	}
}

// RequireRankPriority creates a middleware that checks if a user has a rank with at least the specified priority.
func (m *Middleware) RequireRankPriority(minPriority int) func(ctx context.Context, jid string) error {
	return func(ctx context.Context, jid string) error {
		rank, err := m.service.GetUserRank(ctx, jid)
		if err != nil {
			return fmt.Errorf("error getting user rank: %w", err)
		}
		if rank.Priority < minPriority {
			return ErrAccessDenied
		}

		return nil
	}
}
