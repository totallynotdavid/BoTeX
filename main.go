package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"botex/pkg/auth"
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
	client         *whatsmeow.Client
	commandHandler *commands.CommandHandler
	config         *config.Config
	logger         *logger.Logger
	loggerFactory  *logger.Factory
	shutdownSignal chan os.Signal
	authService    auth.Auth
	db             *sql.DB
}

func NewBot(cfg *config.Config, loggerFactory *logger.Factory) (*Bot, error) {
	appLogger := loggerFactory.GetLogger("bot")

	database, err := setupDatabase(cfg, appLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to setup database: %w", err)
	}

	var successfulInit bool

	defer func() {
		if !successfulInit {
			_ = database.Close()
		}
	}()

	ctx := context.Background()

	appLogger.Info("Initializing database schema", nil)

	if err := auth.InitSchema(ctx, database); err != nil {
		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	appLogger.Info("Database schema initialization completed", nil)

	client, err := setupWhatsAppClient(cfg, loggerFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to setup WhatsApp client: %w", err)
	}

	authService := auth.New(database)

	commandHandler, err := setupCommands(client, cfg, loggerFactory, authService)
	if err != nil {
		return nil, fmt.Errorf("failed to setup commands: %w", err)
	}

	successfulInit = true

	return &Bot{
		client:         client,
		commandHandler: commandHandler,
		config:         cfg,
		logger:         appLogger,
		loggerFactory:  loggerFactory,
		shutdownSignal: make(chan os.Signal, 1),
		authService:    authService,
		db:             database,
	}, nil
}

func setupDatabase(cfg *config.Config, logger *logger.Logger) (*sql.DB, error) {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "file:botex.db?_foreign_keys=on&_journal_mode=WAL"
	}

	logger.Info("Opening database connection", map[string]interface{}{
		"path": dbPath,
	})

	database, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(3600) // 1 hour
	database.SetConnMaxIdleTime(1800) // 30 minutes

	if err := database.PingContext(context.Background()); err != nil {
		_ = database.Close()

		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("Database connection established", nil)

	return database, nil
}

func setupWhatsAppClient(cfg *config.Config, loggerFactory *logger.Factory) (*whatsmeow.Client, error) {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "file:botex.db?_foreign_keys=on&_journal_mode=WAL"
	}

	dbLog := loggerFactory.CreateWhatsmeowLogger("Database", cfg.Logging.Level.String())
	clientLog := loggerFactory.CreateWhatsmeowLogger("Client", cfg.Logging.Level.String())

	container, err := sqlstore.New(context.Background(), "sqlite3", dbPath, dbLog)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow sqlstore initialization failed: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get device store: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, clientLog)

	return client, nil
}

func setupCommands(client *whatsmeow.Client, cfg *config.Config, loggerFactory *logger.Factory, authService auth.Auth) (*commands.CommandHandler, error) {
	registry := commands.NewCommandRegistry(loggerFactory)

	timeLogger := loggerFactory.GetLogger("timing")
	timeTracker := timing.NewTrackerFromConfig(cfg, timeLogger)

	helpCmd := commands.NewHelpCommand(client, cfg, loggerFactory)
	latexCmd := commands.NewLaTeXCommand(client, cfg, timeTracker, loggerFactory)

	registry.Register(helpCmd)
	registry.Register(latexCmd)

	commandHandler, err := commands.NewCommandHandler(client, cfg, registry, loggerFactory, authService)
	if err != nil {
		return nil, fmt.Errorf("failed to create command handler: %w", err)
	}

	helpCmd.SetHandler(commandHandler)

	return commandHandler, nil
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

	if b.commandHandler != nil {
		b.commandHandler.Close()
	}

	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect()
	}

	if b.db != nil {
		err := b.db.Close()
		if err != nil {
			b.logger.Error("Error closing database connection", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	b.logger.Info("Shutdown complete", nil)

	err := b.loggerFactory.Close()
	if err != nil {
		log.Printf("Error closing logger factory: %v", err)
	}

	close(b.shutdownSignal)
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
	if err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: could not load .env file: %v", err)
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

	signal.Notify(bot.shutdownSignal, os.Interrupt, syscall.SIGTERM)
	<-bot.shutdownSignal

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
