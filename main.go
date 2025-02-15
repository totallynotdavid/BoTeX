package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

const latexTemplate = `
\documentclass[preview,border=2pt,convert={density=300,outext=.png}]{standalone}
\usepackage{amsmath}
\usepackage{amsfonts}
\usepackage{physics}
\usepackage{bm}
\begin{document}

\begin{align*}
%s
\end{align*}

\end{document}
`

type MyClient struct {
	WAClient *whatsmeow.Client
}

func transformLatexToImage(latexCode string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "latexbot")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	latexDocument := fmt.Sprintf(latexTemplate, latexCode)
	inputPath := filepath.Join(tempDir, "input.tex")
	if err := os.WriteFile(inputPath, []byte(latexDocument), 0644); err != nil {
		return nil, fmt.Errorf("failed to write latex file: %w", err)
	}

	cmd := exec.Command("pdflatex", "-output-directory", tempDir, "-jobname", "latex", inputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdflatex failed: %s - %w", string(output), err)
	}

	pdfPath := filepath.Join(tempDir, "latex.pdf")
	pngPath := filepath.Join(tempDir, "latex.png")
	cmd = exec.Command("convert", "-density", "300", "-trim", "-background", "white",
		"-alpha", "remove", pdfPath, "-quality", "100", pngPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("convert failed: %s - %w", string(output), err)
	}

	img, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output image: %w", err)
	}

	return img, nil
}

func (mycli *MyClient) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		newMessage := v.Message
		msg := newMessage.GetConversation()
		fmt.Println("Message from:", v.Info.Sender, "->", msg)
		if msg == "" {
			return
		}

		if strings.HasPrefix(msg, "!latex") {
			latexCode := strings.TrimSpace(strings.TrimPrefix(msg, "!latex"))
			if latexCode == "" {
				return
			}

			img, err := transformLatexToImage(latexCode)
			if err != nil {
				fmt.Println("Error generating image:", err)
				return
			}

			resp, err := mycli.WAClient.Upload(context.Background(), img, whatsmeow.MediaImage)
			if err != nil {
				fmt.Println("Error uploading image:", err)
				return
			}

			imageMsg := &waProto.ImageMessage{
				Caption:       proto.String("Generado por @boTeX"),
				Mimetype:      proto.String("image/png"),
				URL:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSHA256: resp.FileEncSHA256,
				FileSHA256:    resp.FileSHA256,
			}

			_, err = mycli.WAClient.SendMessage(context.Background(), v.Info.Sender, &waProto.Message{
				ImageMessage: imageMsg,
			})
			if err != nil {
				fmt.Println("Error sending message:", err)
			} else {
				fmt.Println("Image sent successfully")
			}
		}
	}
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
	mycli := &MyClient{WAClient: client}
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
