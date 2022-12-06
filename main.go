package main

import (
	// bytes.Buffer
	"context"
	"fmt" // prints stuff
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
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
	WAClient       *whatsmeow.Client
	eventHandlerID uint32
}

type MediaType string

type UploadResponse struct {
	URL        string `json:"url"`
	DirectPath string `json:"direct_path"`

	MediaKey      []byte `json:"-"`
	FileEncSHA256 []byte `json:"-"`
	FileSHA256    []byte `json:"-"`
	FileLength    uint64 `json:"-"`
}

func (mycli *MyClient) register() {
	mycli.eventHandlerID = mycli.WAClient.AddEventHandler(mycli.eventHandler)
}

func (mycli *MyClient) Upload(ctx context.Context, data []byte) (*UploadResponse, error) {
	// Use the whatsmeow library's Upload function to upload the image
	mediaInfo, err := mycli.WAClient.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return nil, err
	}

	// Create a new UploadResponse and populate it with the relevant data from the mediaInfo
	response := &UploadResponse{
		URL:           mediaInfo.URL,
		DirectPath:    mediaInfo.DirectPath,
		MediaKey:      mediaInfo.MediaKey,
		FileEncSHA256: mediaInfo.FileEncSHA256,
		FileLength:    mediaInfo.FileLength,
	}

	return response, nil
}

func transformLatexToImage(latexCode string) ([]byte, error) {
	// Use the strings.Replace function to insert the LaTeX code into the template document
	latexDocument := fmt.Sprintf(latexTemplate, latexCode)

	// Write the LaTeX code to a file
	err := ioutil.WriteFile("input.tex", []byte(latexDocument), 0755)
	if err != nil {
		fmt.Println("Error:", err)
		return nil, err
	}

	// Use pdflatex to convert LaTeX code to a PDF file
	cmd := exec.Command("pdflatex", "-output-directory=output", "-jobname=latex", "input.tex")

	// Execute the pdflatex command
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error:", err)
		fmt.Println("Output:", string(output))
		return nil, err
	}

	// Create the output directory if it does not exist
	if _, err := os.Stat("output"); os.IsNotExist(err) {
		os.Mkdir("output", 0755)
	}

	// Use imagemagick to convert PDF file to an image file
	// Use the = operator to assign a value to the cmd variable
	cmd = exec.Command("convert", "-density", "300", "-trim", "-background", "white", "-alpha", "remove", "output/latex.pdf", "-quality", "100", "output/latex.png")

	// Execute the commands
	output, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error:", err)
		fmt.Println("Output:", string(output))
		return nil, err
	}

	// Read the image file into a variable
	img, err := ioutil.ReadFile("output/latex.png")
	if err != nil {
		return nil, err
	}

	return img, nil
}

func (mycli *MyClient) eventHandler(evt interface{}) {

	switch v := evt.(type) {
	case *events.Message:
		newMessage := v.Message
		msg := newMessage.GetConversation()
		fmt.Println("Message from:", v.Info.Sender.User, "->", msg)
		if msg == "" {
			return
		}

		if strings.HasPrefix(msg, "!latex") {
			// Use the strings.TrimPrefix function to remove the "!latex" prefix from the input string
			latexCode := strings.TrimPrefix(msg, "!latex")
			fmt.Println(latexCode)

			// Transform LaTeX code to an image
			img, err := transformLatexToImage(latexCode)
			if err != nil {
				fmt.Println("Error:", err)
			}

			// Use the image as needed
			fmt.Println(img)

			// Read the image from the output/latex.png file
			imgBytes, err := ioutil.ReadFile("output/latex.png")
			if err != nil {
				fmt.Println("Error:", err)
			}

			resp, err := mycli.WAClient.Upload(context.Background(), imgBytes, whatsmeow.MediaImage)

			imageMsg := &waProto.ImageMessage{
				Caption:  proto.String("Generado por @boTeX"),
				Mimetype: proto.String("image/png"),

				Url:           &resp.URL,
				DirectPath:    &resp.DirectPath,
				MediaKey:      resp.MediaKey,
				FileEncSha256: resp.FileEncSHA256,
				FileSha256:    resp.FileSHA256,
				FileLength:    &resp.FileLength,
			}

			response := &waProto.Message{ImageMessage: imageMsg}
			fmt.Println("Sending message:", response)

			userJid := types.NewJID(v.Info.Sender.User, types.DefaultUserServer)
			fmt.Println("Sending message to:", userJid)
			fmt.Println("Attempting to send image")
			mycli.WAClient.SendMessage(context.Background(), userJid, "", response)
			fmt.Println("Image sent")

		}

	}
}

func main() {
	dbLog := waLog.Stdout("Database", "WARN", false)

	// Make sure you add appropriate DB connector imports, e.g. github.com/mattn/go-sqlite3 for SQLite
	container, err := sqlstore.New("sqlite3", "file:examplestore.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "WARN", false)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	// add the eventHandler
	mycli := &MyClient{WAClient: client}
	mycli.register()

	if client.Store.ID == nil {
		// No ID stored, new login
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
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
		// Already logged in, just connect
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// Listen to Ctrl+C (you can also do something else that prevents the program from exiting)
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
