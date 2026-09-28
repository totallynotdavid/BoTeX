package auth

import (
	"context"
	"database/sql"
	"errors"
)

type Service struct {
	repo *Repository
}

// New creates the tables in database if they are missing and returns a Service
// over them. The owner rank always exists; defaults are the app's other ranks,
// and a rank already stored is left as it is.
func New(ctx context.Context, database *sql.DB, defaults ...Rank) (*Service, error) {
	err := initSchema(ctx, database, defaults)
	if err != nil {
		return nil, err
	}

	return &Service{repo: NewRepository(database)}, nil
}

// CheckPermission reports whether user is registered with a rank that may run
// command. An unregistered user is denied without an error.
func (s *Service) CheckPermission(ctx context.Context, user, command string) (bool, error) {
	err := ValidateCommand(command)
	if err != nil {
		return false, err
	}

	found, err := s.repo.GetUser(ctx, user)
	if errors.Is(err, ErrUserNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	rank, err := s.repo.GetRank(ctx, found.Rank)
	if err != nil {
		return false, err
	}

	return rank.HasCommand(command), nil
}

func (s *Service) RegisterUser(ctx context.Context, userID, rankName, registeredBy string) error {
	err := ValidateRankName(rankName)
	if err != nil {
		return err
	}

	_, err = s.repo.GetRank(ctx, rankName)
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
