package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// groupServer is the server part of a group's JID.
const groupServer = "@g.us"

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

// Decision is the outcome of [Service.Authorize]. Its zero value is Undecided,
// so a Decision nobody set never grants a command.
type Decision int

const (
	// Undecided is what Authorize returns with an error: nothing was decided.
	Undecided Decision = iota
	Allowed
	// UserNotRegistered means the user has no rank.
	UserNotRegistered
	// RankLacksCommand means the user's rank does not list the command.
	RankLacksCommand
	// GroupNotRegistered means the command was sent in a group that is not
	// registered and active.
	GroupNotRegistered
)

// Authorize decides whether user may run command. group is the group the
// command was sent in, or "" for a direct chat, which needs no registration.
// A denial is a Decision, not an error; the checks run in the order of the
// constants, so an unregistered user in an unregistered group is told about
// the user.
func (s *Service) Authorize(ctx context.Context, user, group, command string) (Decision, error) {
	err := ValidateCommand(command)
	if err != nil {
		return Undecided, err
	}

	found, err := s.repo.GetUser(ctx, user)
	if errors.Is(err, ErrUserNotFound) {
		return UserNotRegistered, nil
	}

	if err != nil {
		return Undecided, err
	}

	rank, err := s.repo.GetRank(ctx, found.Rank)
	if err != nil {
		return Undecided, err
	}

	if !rank.HasCommand(command) {
		return RankLacksCommand, nil
	}

	if group == "" {
		return Allowed, nil
	}

	_, err = s.repo.GetGroup(ctx, group)
	if errors.Is(err, ErrGroupNotRegistered) {
		return GroupNotRegistered, nil
	}

	if err != nil {
		return Undecided, err
	}

	return Allowed, nil
}

// RegisterUser gives userID the rank rankName. registeredBy says who did it:
// a JID or a label such as "cli". A user who was deactivated is registered
// again with the new rank. One who is active fails with ErrUserExists.
func (s *Service) RegisterUser(ctx context.Context, userID, rankName, registeredBy string) error {
	userID, err := ParseJID(userID)
	if err != nil {
		return err
	}

	err = ValidateRankName(rankName)
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

// RegisterGroup lets commands run in the group groupID, a JID ending in
// "@g.us". registeredBy says who did it: a JID or a label such as "cli". A
// group that was deactivated is registered again. One that is active fails
// with ErrGroupExists.
func (s *Service) RegisterGroup(ctx context.Context, groupID, registeredBy string) error {
	groupID, err := ParseJID(groupID)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(groupID, groupServer) {
		return fmt.Errorf("%w: %q is not a group, want a JID ending in %s", ErrInvalidJID, groupID, groupServer)
	}

	exists, err := s.repo.GroupExists(ctx, groupID)
	if err != nil {
		return err
	}

	if exists {
		return ErrGroupExists
	}

	return s.repo.CreateGroup(ctx, groupID, registeredBy)
}

// DeactivateUser stops userID from running commands. The row stays, so
// SeedOwners leaves the user deactivated. It fails with ErrUserNotFound for a
// user who is not active.
func (s *Service) DeactivateUser(ctx context.Context, userID string) error {
	return s.repo.DeactivateUser(ctx, userID)
}

// DeactivateGroup stops commands from running in groupID. It fails with
// ErrGroupNotRegistered for a group that is not active.
func (s *Service) DeactivateGroup(ctx context.Context, groupID string) error {
	return s.repo.DeactivateGroup(ctx, groupID)
}

// ListUsers returns the active users, oldest registration first.
func (s *Service) ListUsers(ctx context.Context) ([]*User, error) {
	return s.repo.ListUsers(ctx)
}

// ListGroups returns the active groups, oldest registration first.
func (s *Service) ListGroups(ctx context.Context) ([]*Group, error) {
	return s.repo.ListGroups(ctx)
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
