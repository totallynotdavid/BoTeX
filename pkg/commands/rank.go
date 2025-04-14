package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"botex/pkg/auth"
	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"go.mau.fi/whatsmeow"
)

const (
	minCommandArgs = 2
	minSetRankArgs = 2
	minInfoArgs    = 1
)

var (
	ErrInvalidCommandFormat = errors.New("invalid command format")
	ErrUnknownSubcommand    = errors.New("unknown subcommand")
	ErrInvalidSetRankFormat = errors.New("invalid format for set rank command")
	ErrInvalidInfoFormat    = errors.New("invalid format for info command")
)

type RankCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	authService   *auth.Service
}

func NewRankCommand(client *whatsmeow.Client, cfg *config.Config, authService *auth.Service, loggerFactory *logger.LoggerFactory) *RankCommand {
	return &RankCommand{
		config:        cfg,
		messageSender: message.NewMessageSender(client),
		logger:        loggerFactory.GetLogger("rank-command"),
		authService:   authService,
	}
}

func (rc *RankCommand) Name() string {
	return "rank"
}

func (rc *RankCommand) RequiredPermission() string {
	return "manage_ranks"
}

func (rc *RankCommand) Info() CommandInfo {
	return CommandInfo{
		Description: "Manage user ranks and permissions",
		Usage:       "!rank <subcommand> [args]",
		Examples: []string{
			"!rank set <user> <rank>",
			"!rank list",
			"!rank info <user>",
		},
	}
}

func (rc *RankCommand) Handle(ctx context.Context, msg *message.Message) error {
	args := strings.Fields(msg.Text)
	if len(args) < minCommandArgs {
		return fmt.Errorf("%w. Usage: %s", ErrInvalidCommandFormat, rc.Info().Usage)
	}

	subcommand := args[1]
	switch subcommand {
	case "set":
		return rc.handleSetRank(ctx, msg, args[2:])
	case "list":
		return rc.handleListRanks(ctx, msg)
	case "info":
		return rc.handleUserInfo(ctx, msg, args[2:])
	default:
		return fmt.Errorf("%w: %s", ErrUnknownSubcommand, subcommand)
	}
}

func (rc *RankCommand) handleSetRank(ctx context.Context, msg *message.Message, args []string) error {
	if len(args) < minSetRankArgs {
		return fmt.Errorf("%w. Usage: !rank set <user> <rank>", ErrInvalidSetRankFormat)
	}

	userJID := args[0]
	rankName := args[1]

	// Get rank ID from name
	rank, err := rc.authService.GetRankByName(ctx, rankName)
	if err != nil {
		return fmt.Errorf("failed to get rank: %w", err)
	}

	// Set user's rank
	if err := rc.authService.SetUserRank(ctx, userJID, rank.ID); err != nil {
		return fmt.Errorf("failed to set rank: %w", err)
	}

	response := fmt.Sprintf("Successfully set rank of %s to %s", userJID, rankName)

	if err := rc.messageSender.SendText(ctx, msg.Recipient, response); err != nil {
		return fmt.Errorf("failed to send rank update message: %w", err)
	}

	return nil
}

func (rc *RankCommand) handleListRanks(ctx context.Context, msg *message.Message) error {
	ranks, err := rc.authService.GetAllRanks(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ranks: %w", err)
	}

	var response strings.Builder
	response.WriteString("Available ranks:\n")
	for _, rank := range ranks {
		response.WriteString(fmt.Sprintf("- %s (Priority: %d)\n", rank.Name, rank.Priority))
	}

	if err := rc.messageSender.SendText(ctx, msg.Recipient, response.String()); err != nil {
		return fmt.Errorf("failed to send ranks list message: %w", err)
	}

	return nil
}

func (rc *RankCommand) handleUserInfo(ctx context.Context, msg *message.Message, args []string) error {
	if len(args) < minInfoArgs {
		return fmt.Errorf("%w. Usage: !rank info <user>", ErrInvalidInfoFormat)
	}

	userJID := args[0]
	rank, err := rc.authService.GetUserRank(ctx, userJID)
	if err != nil {
		return fmt.Errorf("failed to get user rank: %w", err)
	}

	permissions, err := rc.authService.GetUserPermissions(ctx, userJID)
	if err != nil {
		return fmt.Errorf("failed to get user permissions: %w", err)
	}

	var response strings.Builder
	response.WriteString(fmt.Sprintf("User: %s\n", userJID))
	response.WriteString(fmt.Sprintf("Rank: %s (Priority: %d)\n", rank.Name, rank.Priority))
	response.WriteString("Permissions:\n")
	for _, perm := range permissions {
		response.WriteString(fmt.Sprintf("- %s\n", perm.Name))
	}

	if err := rc.messageSender.SendText(ctx, msg.Recipient, response.String()); err != nil {
		return fmt.Errorf("failed to send user info message: %w", err)
	}

	return nil
}
