package commands_test

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/totallynotdavid/botkit/pkg/auth"
	"github.com/totallynotdavid/botkit/pkg/commands"
	"github.com/totallynotdavid/botkit/pkg/config"
	"github.com/totallynotdavid/botkit/pkg/logger"
	"github.com/totallynotdavid/botkit/pkg/message"
)

type fakeCommand struct {
	called bool
}

func (c *fakeCommand) Handle(_ context.Context, _ *message.Message) error {
	c.called = true

	return nil
}

func (c *fakeCommand) Name() string { return "test" }

func (c *fakeCommand) Info() commands.CommandInfo {
	return commands.CommandInfo{Description: "test command"}
}

type allowAllAuth struct{}

func (allowAllAuth) CheckPermission(_ context.Context, _, _, _ string) (*auth.PermissionResult, error) {
	return &auth.PermissionResult{Allowed: true, Reason: "ok"}, nil
}

func (allowAllAuth) RegisterUser(_ context.Context, _, _, _ string) error { return nil }

func (allowAllAuth) RegisterGroup(_ context.Context, _, _ string) error { return nil }

func (allowAllAuth) GetUser(_ context.Context, userID string) (*auth.User, error) {
	return &auth.User{ID: userID, Rank: "owner"}, nil
}

func (allowAllAuth) GetRank(_ context.Context, rankName string) (*auth.Rank, error) {
	return &auth.Rank{Name: rankName, Commands: []string{"*"}}, nil
}

func (allowAllAuth) GetGroup(_ context.Context, groupID string) (*auth.Group, error) {
	return &auth.Group{ID: groupID}, nil
}

func (allowAllAuth) ListRanks(_ context.Context) ([]*auth.Rank, error) {
	return nil, nil
}

func newTestHandler(t *testing.T, processOwnMessages bool) (*commands.CommandHandler, *fakeCommand) {
	t.Helper()

	cfg := &config.Config{}
	cfg.MaxConcurrent = 10
	cfg.RateLimit.Requests = 1000
	cfg.RateLimit.Period = time.Minute
	cfg.RateLimit.NotificationCooldown = time.Minute
	cfg.RateLimit.CleanupInterval = time.Hour
	cfg.Timing.Level = "disabled"
	cfg.Auth.ProcessOwnMessages = processOwnMessages

	loggerFactory, err := logger.NewFactory(logger.Config{
		Level:     logger.ParseLogLevel("ERROR"),
		Directory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("failed to create logger factory: %v", err)
	}

	t.Cleanup(func() {
		closeErr := loggerFactory.Close()
		if closeErr != nil {
			t.Errorf("failed to close logger factory: %v", closeErr)
		}
	})

	registry := commands.NewCommandRegistry(loggerFactory)
	cmd := &fakeCommand{}
	registry.Register(cmd)

	handler, err := commands.NewCommandHandler(nil, cfg, registry, loggerFactory, allowAllAuth{})
	if err != nil {
		t.Fatalf("failed to create command handler: %v", err)
	}

	t.Cleanup(handler.Close)

	return handler, cmd
}

func newTestMessageEvent(isFromMe bool) *events.Message {
	sender := types.NewJID("15551234567", types.DefaultUserServer)
	text := "!test hello"

	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     sender,
				Sender:   sender,
				IsFromMe: isFromMe,
				IsGroup:  false,
			},
			ID: "test-message-id",
		},
		Message: &waE2E.Message{
			Conversation: &text,
		},
	}
}

func TestHandleEvent_IgnoresOwnMessagesByDefault(t *testing.T) {
	t.Parallel()

	handler, cmd := newTestHandler(t, false)

	handler.HandleEvent(newTestMessageEvent(true))

	if cmd.called {
		t.Error("expected command not to be dispatched for a message from the bot itself")
	}
}

func TestHandleEvent_DispatchesOwnMessagesWhenOptedIn(t *testing.T) {
	t.Parallel()

	handler, cmd := newTestHandler(t, true)

	handler.HandleEvent(newTestMessageEvent(true))

	if !cmd.called {
		t.Error("expected command to be dispatched when opted in to self messages")
	}
}

func TestHandleEvent_DispatchesMessagesFromOthers(t *testing.T) {
	t.Parallel()

	handler, cmd := newTestHandler(t, false)

	handler.HandleEvent(newTestMessageEvent(false))

	if !cmd.called {
		t.Error("expected command to be dispatched for a message from someone else")
	}
}
