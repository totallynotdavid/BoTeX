package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"botex/pkg/commands"
)

type MyClient struct {
	WAClient       *whatsmeow.Client
	commandHandler *commands.CommandHandler
}

func (mycli *MyClient) eventHandler(evt interface{}) {
	mycli.commandHandler.HandleEvent(evt)
}

func main() {
	dbLog := waLog.Stdout("Database", "WARN", false)
	container, err := sqlstore.New("sqlite3", "file:examplestore.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "WARN", false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	mycli := &MyClient{
		WAClient:       client,
		commandHandler: commands.NewCommandHandler(client),
	}

	client.AddEventHandler(mycli.eventHandler)

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		if err = client.Connect(); err != nil {
			panic(err)
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
			panic(err)
		}
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
