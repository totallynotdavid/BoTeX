package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"botex/pkg/config"
	"botex/pkg/message"
)

type LaTeXCommand struct {
	client *whatsmeow.Client
	config *config.Config
}

func NewLaTeXCommand(client *whatsmeow.Client, config *config.Config) *LaTeXCommand {
	return &LaTeXCommand{
		client: client,
		config: config,
	}
}

func (lc *LaTeXCommand) Handle(ctx context.Context, msg *message.Message) error {
	text := msg.GetText()
	if !strings.HasPrefix(text, "!latex") {
		return nil
	}

	latexCode := strings.TrimSpace(strings.TrimPrefix(text, "!latex"))
	if latexCode == "" {
		return fmt.Errorf("empty LaTeX code")
	}

	if len(latexCode) > 1000 {
		return fmt.Errorf("LaTeX code too long (max 1000 characters)")
	}

	// Not allowed to use \input or \include
	if strings.Contains(latexCode, "\\input") || strings.Contains(latexCode, "\\include") {
		return fmt.Errorf("unsafe LaTeX commands not allowed")
	}

	imgWebP, err := lc.transformLatexToImage(latexCode)
	if err != nil {
		return fmt.Errorf("error generating image: %w", err)
	}

	// Validate image size
	if int64(len(imgWebP)) > lc.config.MaxImageSize {
		return fmt.Errorf("generated image too large (max %d bytes)", lc.config.MaxImageSize)
	}

	resp, err := lc.client.Upload(ctx, imgWebP, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("error uploading sticker: %w", err)
	}

	stickerMsg := &waE2E.StickerMessage{
		Mimetype:      proto.String("image/webp"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
	}

	_, err = lc.client.SendMessage(ctx, msg.Recipient, &waE2E.Message{
		StickerMessage: stickerMsg,
	})
	if err != nil {
		return fmt.Errorf("error sending sticker: %w", err)
	}

	return nil
}

func (lc *LaTeXCommand) transformLatexToImage(latexCode string) ([]byte, error) {
	tempDir, err := os.MkdirTemp(lc.config.TempDir, "latexbot")
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
		"-alpha", "remove", "-border", "8x8", "-bordercolor", "white", pdfPath, "-quality", "100", pngPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("convert failed: %s - %w", string(output), err)
	}

	imgPNG, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output image: %w", err)
	}

	imgWebP, err := lc.convertPNGtoWebP(imgPNG)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to webp: %w", err)
	}

	return imgWebP, nil
}

func (lc *LaTeXCommand) convertPNGtoWebP(pngData []byte) ([]byte, error) {
	tempDir, err := os.MkdirTemp(lc.config.TempDir, "webpconv")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	pngPath := filepath.Join(tempDir, "input.png")
	if err := os.WriteFile(pngPath, pngData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write png file: %w", err)
	}

	webpPath := filepath.Join(tempDir, "output.webp")
	cmd := exec.Command("convert", pngPath, webpPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("convert failed: %s - %w", string(output), err)
	}

	webpData, err := os.ReadFile(webpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output webp: %w", err)
	}

	return webpData, nil
}

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
