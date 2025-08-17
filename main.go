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
	client          *whatsmeow.Client
	commandHandler  *commands.CommandHandler
	config          *config.Config
	logger          *logger.Logger
	loggerFactory   *logger.Factory
	shutdownSignals chan os.Signal
	authService     auth.AuthService
	db              *sql.DB
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

	// Create database connection for unified auth module
	// Parse the database path to get a clean connection string
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "file:botex.db?_foreign_keys=on&_journal_mode=WAL"
	}
	
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database for unified auth module: %w", err)
	}
	
	// Test the connection
	err = db.Ping()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database for unified auth module: %w", err)
	}
	
	// Initialize fresh database schema with default ranks
	ctx := context.Background()
	appLogger.Info("Initializing fresh database schema with default ranks", nil)
	
	err = auth.InitializeFreshSchema(ctx, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize fresh database schema: %w", err)
	}
	
	appLogger.Info("Database schema initialization completed successfully", nil)

	// Create WhatsApp client adapter for auth store
	whatsappClientAdapter := auth.NewWhatsmeowClientAdapter(client)

	// Initialize unified auth store
	authStore, err := auth.NewSQLiteAuthStore(db, loggerFactory, whatsappClientAdapter)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create auth store: %w", err)
	}

	// Initialize unified auth service
	authService, err := auth.NewUnifiedAuthService(authStore, loggerFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create unified auth service: %w", err)
	}

	registry := commands.NewCommandRegistry(loggerFactory)

	timeLogger := loggerFactory.GetLogger("timing")
	timeTracker := timing.NewTrackerFromConfig(cfg, timeLogger)

	helpCmd := commands.NewHelpCommand(client, cfg, loggerFactory)
	latexCmd := commands.NewLaTeXCommand(client, cfg, timeTracker, loggerFactory)

	registry.Register(helpCmd)
	registry.Register(latexCmd)

	// Initialize command handler with unified auth service
	commandHandler, err := commands.NewCommandHandler(client, cfg, registry, loggerFactory, authService)
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
		authService:     authService,
		db:              db,
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

	b.commandHandler.Close()

	if b.client.IsConnected() {
		b.client.Disconnect()
	}

	// Close unified auth service
	if b.authService != nil {
		err := b.authService.Close()
		if err != nil {
			b.logger.Error("Error closing unified auth service", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}



	// Close database connection
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
