package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"botex/pkg/commands"
	"botex/pkg/config"
)

type MyClient struct {
	WAClient       *whatsmeow.Client
	commandHandler *commands.CommandHandler
	config         *config.Config
	stopCleanup    chan struct{}
}

func (mycli *MyClient) eventHandler(evt interface{}) {
	mycli.commandHandler.HandleEvent(evt)
}

func (mycli *MyClient) startCleanup() {
	ticker := time.NewTicker(mycli.config.CleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mycli.cleanupTempFiles()
		case <-mycli.stopCleanup:
			return
		}
	}
}

func (mycli *MyClient) cleanupTempFiles() {
	now := time.Now()
	dirs, err := filepath.Glob(filepath.Join(mycli.config.TempDir, "latexbot*"))
	if err != nil {
		fmt.Printf("Error finding temp directories: %v\n", err)
		return
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > 24*time.Hour {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Printf("Error removing temp directory %s: %v\n", dir, err)
			}
		}
	}
}

func main() {
	config := config.Load()

	dbLog := waLog.Stdout("Database", config.LogLevel, false)
	container, err := sqlstore.New("sqlite3", config.DBPath, dbLog)
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		fmt.Printf("Failed to get device store: %v\n", err)
		os.Exit(1)
	}

	clientLog := waLog.Stdout("Client", config.LogLevel, false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	mycli := &MyClient{
		WAClient:       client,
		commandHandler: commands.NewCommandHandler(client, config),
		config:         config,
		stopCleanup:    make(chan struct{}),
	}

	client.AddEventHandler(mycli.eventHandler)

	// Start cleanup routine
	go mycli.startCleanup()

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		if err = client.Connect(); err != nil {
			fmt.Printf("Failed to connect: %v\n", err)
			os.Exit(1)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		if err = client.Connect(); err != nil {
			fmt.Printf("Failed to connect: %v\n", err)
			os.Exit(1)
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	close(mycli.stopCleanup)
	client.Disconnect()
}
