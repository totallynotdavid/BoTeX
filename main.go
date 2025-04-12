package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"botex/pkg/commands"
	"botex/pkg/config"
	"botex/pkg/logger"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Bot struct {
	client          *whatsmeow.Client
	commandHandler  *commands.CommandHandler
	config          *config.Config
	logger          *logger.Logger
	shutdownSignals chan os.Signal
}

func NewBot(cfg *config.Config) (*Bot, error) {
	// Initialize database
	dbLog := waLog.Stdout("Database", cfg.LogLevel, false)
	container, err := sqlstore.New("sqlite3", cfg.DBPath, dbLog)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}

	// Get device store
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to get device store: %w", err)
	}

	// Create WhatsApp client
	clientLog := waLog.Stdout("Client", cfg.LogLevel, false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Create command handler
	commandHandler, err := commands.NewCommandHandler(client, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create command handler: %w", err)
	}

	return &Bot{
		client:          client,
		commandHandler:  commandHandler,
		config:          cfg,
		logger:          logger.NewLogger(logger.INFO),
		shutdownSignals: make(chan os.Signal, 1),
	}, nil
}

func (b *Bot) Start() error {
	b.logger.Info("Starting bot", nil)

	b.client.AddEventHandler(b.commandHandler.HandleEvent)

	if b.client.Store.ID == nil {
		b.logger.Info("No device stored, initiating QR login", nil)

		return b.handleQRLogin()
	}

	b.logger.Info("Restoring existing session", nil)

	return b.connect()
}

func (b *Bot) handleQRLogin() error {
	qrChan, _ := b.client.GetQRChannel(context.Background())
	if err := b.client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			b.logger.Info("QR login successful", nil)

			return nil
		case "timeout":
			return errors.New("QR login timed out")
		}
	}

	return nil
}

func (b *Bot) connect() error {
	if err := b.client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown() {
	b.logger.Info("Initiating graceful shutdown", nil)
	defer b.logger.Info("Shutdown complete", nil)

	b.commandHandler.Close()

	if b.client.IsConnected() {
		b.client.Disconnect()
	}

	close(b.shutdownSignals)
}

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize bot
	bot, err := NewBot(cfg)
	if err != nil {
		fmt.Printf("Failed to initialize bot: %v\n", err)
		os.Exit(1)
	}

	// Start the bot
	if err := bot.Start(); err != nil {
		bot.logger.Error("Failed to start bot", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	// Handle shutdown signals
	signal.Notify(bot.shutdownSignals, os.Interrupt, syscall.SIGTERM)
	<-bot.shutdownSignals

	bot.Shutdown()
}
