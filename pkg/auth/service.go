package auth

import (
	"context"
	"database/sql"
	"errors"
)

type Service struct {
	repo *Repository
}

func NewService(db *sql.DB) *Service {
	return &Service{
		repo: NewRepository(db),
	}
}

func (s *Service) CheckPermission(ctx context.Context, userID, groupID, command string) (*PermissionResult, error) {
	if err := ValidateCommand(command); err != nil {
		return nil, err
	}

	user, err := s.repo.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return &PermissionResult{
				Allowed: false,
				Reason:  "User not registered",
			}, nil
		}

		return nil, err
	}

	if groupID != "" {
		exists, err := s.repo.GroupExists(ctx, groupID)
		if err != nil {
			return nil, err
		}

		if !exists {
			return &PermissionResult{
				Allowed: false,
				Reason:  "Group not registered",
			}, nil
		}
	}

	rank, err := s.repo.GetRank(ctx, user.Rank)
	if err != nil {
		return nil, err
	}

	if !rank.HasCommand(command) {
		return &PermissionResult{
			Allowed:  false,
			Reason:   "Command not allowed for your rank",
			UserRank: user.Rank,
		}, nil
	}

	return &PermissionResult{
		Allowed:  true,
		Reason:   "Access granted",
		UserRank: user.Rank,
	}, nil
}

func (s *Service) RegisterUser(ctx context.Context, userID, rankName, registeredBy string) error {
	if err := ValidateRankName(rankName); err != nil {
		return err
	}

	_, err := s.repo.GetRank(ctx, rankName)
	if err != nil {
		if errors.Is(err, ErrRankNotFound) {
			return ErrRankNotFound
		}
		return err
	}

	exists, err := s.repo.UserExists(ctx, userID)
	if err != nil {
		return err
	}

	if exists {
		return ErrUserExists
	}

	return s.repo.CreateUser(ctx, userID, rankName, registeredBy)
}

func (s *Service) RegisterGroup(ctx context.Context, groupID, registeredBy string) error {
	if groupID == "" {
		return ErrInvalidInput
	}

	exists, err := s.repo.UserExists(ctx, registeredBy)
	if err != nil {
		return err
	}

	if !exists {
		return ErrUserNotFound
	}

	exists, err = s.repo.GroupExists(ctx, groupID)
	if err != nil {
		return err
	}

	if exists {
		return ErrGroupExists
	}

	return s.repo.CreateGroup(ctx, groupID, registeredBy)
}

func (s *Service) GetUser(ctx context.Context, userID string) (*User, error) {
	return s.repo.GetUser(ctx, userID)
}

func (s *Service) GetRank(ctx context.Context, rankName string) (*Rank, error) {
	err := ValidateRankName(rankName)
	if err != nil {
		return nil, err
	}

	return s.repo.GetRank(ctx, rankName)
}

func (s *Service) ListRanks(ctx context.Context) ([]*Rank, error) {
	return s.repo.ListRanks(ctx)
}

func (s *Service) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	if groupID == "" {
		return nil, ErrInvalidInput
	}

	return s.repo.GetGroup(ctx, groupID)
}
