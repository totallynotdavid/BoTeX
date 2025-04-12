package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"botex/pkg/config"
	"botex/pkg/logger"
	"botex/pkg/message"

	"go.mau.fi/whatsmeow"
)

type LaTeXCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	renderTimeout time.Duration
}

func NewLaTeXCommand(client *whatsmeow.Client, config *config.Config) *LaTeXCommand {
	return &LaTeXCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		renderTimeout: 45 * time.Second, // Timeout for the entire rendering process
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

	renderCtx, cancel := context.WithTimeout(ctx, lc.renderTimeout)
	defer cancel()

	imgWebP, err := lc.renderLatex(renderCtx, latexCode)
	if err != nil {
		if renderCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("LaTeX rendering timed out after %s", lc.renderTimeout)
		}
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

func (lc *LaTeXCommand) execCommand(ctx context.Context, name string, cmd *exec.Cmd) error {
	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	lc.logger.Debug(fmt.Sprintf("%s completed", name), map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
		"command":     cmd.String(),
	})

	if err != nil {
		lc.logger.Error(fmt.Sprintf("%s failed", name), map[string]interface{}{
			"output":      string(output),
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
			"command":     cmd.String(),
		})

		// Check if the context has been canceled or timed out
		if ctx.Err() != nil {
			return fmt.Errorf("%s was interrupted: %w", name, ctx.Err())
		}

		return fmt.Errorf("%s failed: %w", name, err)
	}

	return nil
}

func (lc *LaTeXCommand) renderLatex(ctx context.Context, code string) ([]byte, error) {
	startTime := time.Now()

	tempDir, err := os.MkdirTemp(lc.config.TempDir, "latex-")
	if err != nil {
		return nil, fmt.Errorf("temp dir creation failed: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			lc.logger.Error("Failed to clean up temp directory", map[string]interface{}{
				"error":   err.Error(),
				"tempDir": tempDir,
			})
		}
	}()

	texPath := filepath.Join(tempDir, "equation.tex")
	if err := os.WriteFile(texPath, []byte(lc.createDocument(code)), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write tex file: %w", err)
	}

	// Define output paths
	pdfPath := filepath.Join(tempDir, "equation.pdf")
	pngPath := filepath.Join(tempDir, "equation.png")
	webpPath := filepath.Join(tempDir, "equation.webp")

	// Step 1: Compile LaTeX to PDF
	pdflatexCmd := exec.CommandContext(ctx, "pdflatex", "-output-directory", tempDir, texPath)
	pdflatexCmd.Env = append(os.Environ(), "TEXMFOUTPUT="+tempDir) // Ensure LaTeX writes to temp dir
	if err := lc.execCommand(ctx, "LaTeX compilation", pdflatexCmd); err != nil {
		return nil, err
	}

	// Step 2: Convert PDF to PNG
	convertCmd := exec.CommandContext(ctx, "convert", "-density", "300", pdfPath, "-quality", "90", pngPath)
	if err := lc.execCommand(ctx, "PDF to PNG conversion", convertCmd); err != nil {
		return nil, err
	}

	// Step 3: Convert PNG to WebP
	cwebpCmd := exec.CommandContext(ctx, "cwebp", pngPath, "-o", webpPath)
	if err := lc.execCommand(ctx, "PNG to WebP conversion", cwebpCmd); err != nil {
		return nil, err
	}

	// Read the WebP file
	webpData, err := os.ReadFile(webpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read output image: %w", err)
	}

	// Log total processing time
	totalDuration := time.Since(startTime)
	lc.logger.Info("LaTeX rendering completed", map[string]interface{}{
		"total_duration_ms": totalDuration.Milliseconds(),
		"equation_length":   len(code),
	})

	return webpData, nil
}

func (lc *LaTeXCommand) createDocument(content string) string {
	return `\documentclass[preview]{standalone}
\usepackage{amsmath,amssymb,amsfonts}
\begin{document}
\thispagestyle{empty}
` + content + `
\end{document}`
}
