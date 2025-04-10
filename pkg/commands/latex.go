package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"botex/pkg/config"
	"botex/pkg/message"

	"go.mau.fi/whatsmeow"
)

type LaTeXCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
}

func NewLaTeXCommand(client *whatsmeow.Client, config *config.Config) *LaTeXCommand {
	return &LaTeXCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
	}
}

func (lc *LaTeXCommand) Name() string {
	return "latex"
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

	// Sanitize input
	if err := lc.validateLatex(latexCode); err != nil {
		return err
	}

	imgWebP, err := lc.transformLatexToImage(latexCode)
	if err != nil {
		return fmt.Errorf("error generating image: %w", err)
	}

	// Validate image size
	if int64(len(imgWebP)) > lc.config.MaxImageSize {
		return fmt.Errorf("generated image too large (max %d bytes)", lc.config.MaxImageSize)
	}

	return lc.messageSender.SendImage(ctx, msg.Recipient, imgWebP, latexCode)
}

func (lc *LaTeXCommand) validateLatex(code string) error {
	dangerousCommands := []string{
		"\\input", "\\include", "\\write18", "\\openout", "\\read",
		"\\catcode", "\\def", "\\let", "\\futurelet", "\\newhelp",
		"\\uppercase", "\\lowercase", "\\relax", "\\aftergroup",
		"\\afterassignment", "\\expandafter", "\\noexpand", "\\special",
	}

	for _, cmd := range dangerousCommands {
		if strings.Contains(code, cmd) {
			return fmt.Errorf("unsafe LaTeX command detected: %s", cmd)
		}
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
