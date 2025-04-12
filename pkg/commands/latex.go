package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"
	"go.mau.fi/whatsmeow"
)

type LaTeXCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
}

func NewLaTeXCommand(client *whatsmeow.Client, config *config.Config) *LaTeXCommand {
	return &LaTeXCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
	}
}

func (lc *LaTeXCommand) Name() string {
	return "latex"
}

func (lc *LaTeXCommand) Info() CommandInfo {
	return CommandInfo{
		Description: "Render LaTeX equations into images",
		Usage:       "!latex <equation>",
		Examples: []string{
			"!latex x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}",
			"!latex \\int_{a}^{b} f(x)\\,dx = F(b) - F(a)",
		},
	}
}

func (lc *LaTeXCommand) Handle(ctx context.Context, msg *message.Message) error {
	latexCode := strings.TrimSpace(msg.Text)
	if latexCode == "" {
		return fmt.Errorf("empty LaTeX equation")
	}

	if len(latexCode) > 1000 {
		return fmt.Errorf("LaTeX code exceeds 1000 character limit")
	}

	if err := lc.validateLatex(latexCode); err != nil {
		return err
	}

	imgWebP, err := lc.renderLatex(latexCode)
	if err != nil {
		return fmt.Errorf("rendering failed: %w", err)
	}

	return lc.messageSender.SendImage(ctx, msg.Recipient, imgWebP, "LaTeX Render")
}

func (lc *LaTeXCommand) validateLatex(code string) error {
	blacklist := []string{"\\input", "\\include", "\\write18", "\\def", "\\let"}
	for _, cmd := range blacklist {
		if strings.Contains(code, cmd) {
			return fmt.Errorf("disallowed LaTeX command: %s", cmd)
		}
	}
	return nil
}

func (lc *LaTeXCommand) renderLatex(code string) ([]byte, error) {
	tempDir, err := os.MkdirTemp(lc.config.TempDir, "latex-")
	if err != nil {
		return nil, fmt.Errorf("temp dir creation failed: %w", err)
	}
	defer os.RemoveAll(tempDir)

	texPath := filepath.Join(tempDir, "equation.tex")
	if err := os.WriteFile(texPath, []byte(lc.createDocument(code)), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write tex file: %w", err)
	}

	if output, err := exec.Command("pdflatex", "-output-directory", tempDir, texPath).CombinedOutput(); err != nil {
		lc.logger.Error("LaTeX compilation failed", map[string]interface{}{
			"output": string(output),
			"error":  err.Error(),
		})
		return nil, fmt.Errorf("LaTeX compilation error")
	}

	// Convert PDF to PNG
	pdfPath := filepath.Join(tempDir, "equation.pdf")
	pngPath := filepath.Join(tempDir, "equation.png")
	if output, err := exec.Command("convert", "-density", "300", pdfPath, "-quality", "90", pngPath).CombinedOutput(); err != nil {
		lc.logger.Error("Image conversion failed", map[string]interface{}{
			"output": string(output),
			"error":  err.Error(),
		})
		return nil, fmt.Errorf("image conversion error")
	}

	// Convert PNG to WebP
	webpPath := filepath.Join(tempDir, "equation.webp")
	if output, err := exec.Command("cwebp", pngPath, "-o", webpPath).CombinedOutput(); err != nil {
		lc.logger.Error("WebP conversion failed", map[string]interface{}{
			"output": string(output),
			"error":  err.Error(),
		})
		return nil, fmt.Errorf("WebP conversion error")
	}

	return os.ReadFile(webpPath)
}

func (lc *LaTeXCommand) createDocument(content string) string {
	return `\documentclass[preview]{standalone}
\usepackage{amsmath,amssymb,amsfonts}
\begin{document}
\thispagestyle{empty}
` + content + `
\end{document}`
}
