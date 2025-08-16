package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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
)

var ErrQRLoginTimeout = errors.New("QR login timed out")

type Bot struct {
	client          *whatsmeow.Client
	commandHandler  *commands.CommandHandler
	config          *config.Config
	logger          *logger.Logger
	loggerFactory   *logger.Factory
	shutdownSignals chan os.Signal
}

func NewBot(cfg *config.Config, loggerFactory *logger.Factory) (*Bot, error) {
	appLogger := loggerFactory.GetLogger("bot")

	dbLog := loggerFactory.CreateWhatsmeowLogger("Database", cfg.Logging.Level.String())
	clientLog := loggerFactory.CreateWhatsmeowLogger("Client", cfg.Logging.Level.String())

	container, err := sqlstore.New(context.Background(), "sqlite3", cfg.DBPath, dbLog)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get device store: %w", err)
	}

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
		loggerFactory:   loggerFactory,
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

func (b *Bot) Shutdown() {
	b.logger.Info("Initiating graceful shutdown", nil)
	defer b.logger.Info("Shutdown complete", nil)

	b.commandHandler.Close()

	if b.client.IsConnected() {
		b.client.Disconnect()
	}

	err := b.loggerFactory.Close()
	if err != nil {
		log.Printf("Error closing logger factory: %v", err)
	}

	close(b.shutdownSignals)
}

func (b *Bot) handleQRLogin() error {
	qrChan, err := b.client.GetQRChannel(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get QR channel: %w", err)
	}

	err = b.client.Connect()
	if err != nil {
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
	err := b.client.Connect()
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	return nil
}

func run() error {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Error loading .env file: %v", err)
	}

	cfg := config.Load()

	err = cfg.Validate()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	loggerFactory, err := logger.NewFactory(cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to create logger factory: %w", err)
	}

	defer func() {
		cerr := loggerFactory.Close()
		if cerr != nil {
			log.Printf("Error closing logger factory: %v", cerr)
		}
	}()

	bot, err := NewBot(cfg, loggerFactory)
	if err != nil {
		return fmt.Errorf("failed to initialize bot: %w", err)
	}

	err = bot.Start()
	if err != nil {
		return fmt.Errorf("failed to start bot: %w", err)
	}

	signal.Notify(bot.shutdownSignals, os.Interrupt, syscall.SIGTERM)
	<-bot.shutdownSignals
	bot.Shutdown()

	return nil
}

func main() {
	err := run()
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
