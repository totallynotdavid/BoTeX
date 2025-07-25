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
	"botex/pkg/timing"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var ErrQRLoginTimeout = errors.New("QR login timed out")

type Bot struct {
	client          *whatsmeow.Client
	commandHandler  *commands.CommandHandler
	config          *config.Config
	logger          *logger.Logger
	shutdownSignals chan os.Signal
}

func NewBot(cfg *config.Config, loggerFactory *logger.LoggerFactory) (*Bot, error) {
	appLogger := loggerFactory.GetLogger("bot")

	dbLog := waLog.Stdout("Database", cfg.LogLevel, false)
	container, err := sqlstore.New(context.Background(), "sqlite3", cfg.DBPath, dbLog)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get device store: %w", err)
	}

	clientLog := waLog.Stdout("Client", cfg.LogLevel, false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	registry := commands.NewCommandRegistry(loggerFactory)

	timeLogger := loggerFactory.GetLogger("timing")
	timeTracker := timing.NewTrackerFromConfig(cfg, timeLogger)

	helpCmd := commands.NewHelpCommand(client, cfg, loggerFactory)
	latexCmd := commands.NewLaTeXCommand(client, cfg, timeTracker, loggerFactory)

	registry.Register(helpCmd)
	registry.Register(latexCmd)

	commandHandler, err := commands.NewCommandHandler(client, cfg, registry, loggerFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create command handler: %w", err)
	}

	helpCmd.SetHandler(commandHandler)

	return &Bot{
		client:          client,
		commandHandler:  commandHandler,
		config:          cfg,
		logger:          appLogger,
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
	qrChan, err := b.client.GetQRChannel(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get QR channel: %w", err)
	}

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
			return ErrQRLoginTimeout
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
	loggerFactory := logger.NewLoggerFactory(logger.INFO)
	startupLogger := loggerFactory.GetLogger("startup")

	if err := godotenv.Load(); err != nil {
		startupLogger.Warn("Error loading .env file", map[string]interface{}{
			"error": err.Error(),
		})
	}

	cfg := config.Load(startupLogger)

	if err := cfg.Validate(); err != nil {
		startupLogger.Error("Invalid configuration", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	loggerFactory = logger.NewLoggerFactory(logger.ParseLogLevel(cfg.LogLevel))

	bot, err := NewBot(cfg, loggerFactory)
	if err != nil {
		startupLogger.Error("Failed to initialize bot", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	if err := bot.Start(); err != nil {
		bot.logger.Error("Failed to start bot", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	signal.Notify(bot.shutdownSignals, os.Interrupt, syscall.SIGTERM)
	<-bot.shutdownSignals
	bot.Shutdown()
}
