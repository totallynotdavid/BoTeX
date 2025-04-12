package commands

import (
	"context"
	"errors"
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

const (
	defaultRenderTimeoutSec = 45
	maxLatexCodeLength      = 1000
	secureFilePermissions   = 0o600
	allowedBaseFilename     = "equation"
)

var (
	ErrEmptyLatex         = errors.New("empty LaTeX equation")
	ErrLatexTooLong       = errors.New("LaTeX code exceeds character limit")
	ErrRenderTimeout      = errors.New("LaTeX rendering timed out")
	ErrDisallowedLatexCmd = errors.New("disallowed LaTeX command")
	ErrTempDirCreation    = errors.New("temp dir creation failed")
	ErrWriteTexFile       = errors.New("failed to write tex file")
	ErrReadOutputImage    = errors.New("failed to read output image")
	ErrToolNotFound       = errors.New("required tool not found")
	ErrPathOutsideDir     = errors.New("file path is outside the permitted directory")
	ErrPathNotAbsolute    = errors.New("path is not absolute")
)

type LaTeXCommand struct {
	config        *config.Config
	messageSender *message.MessageSender
	logger        *logger.Logger
	renderTimeout time.Duration
	toolPaths     struct {
		pdflatex string
		convert  string
		cwebp    string
	}
}

type RenderContext struct {
	tempDirectory string
	filePaths     map[string]string
	logger        *logger.Logger
}

func NewLaTeXCommand(client *whatsmeow.Client, config *config.Config) *LaTeXCommand {
	command := &LaTeXCommand{
		config:        config,
		messageSender: message.NewMessageSender(client),
		logger:        logger.NewLogger(logger.INFO),
		renderTimeout: defaultRenderTimeoutSec * time.Second,
	}
	command.initializeToolPaths()

	return command
}

func (lc *LaTeXCommand) initializeToolPaths() {
	resolveToolPath := func(configPath, defaultExecutable string) string {
		if configPath != "" {
			absPath, err := filepath.Abs(configPath)
			if err != nil {
				lc.logger.Error("Absolute path resolution failed",
					map[string]interface{}{"path": configPath, "error": err.Error()})

				return lc.findExecutableInPath(defaultExecutable)
			}

			return absPath
		}

		return lc.findExecutableInPath(defaultExecutable)
	}

	lc.toolPaths.pdflatex = resolveToolPath(lc.config.PDFLatexPath, "pdflatex")
	lc.toolPaths.convert = resolveToolPath(lc.config.ConvertPath, "convert")
	lc.toolPaths.cwebp = resolveToolPath(lc.config.CWebPPath, "cwebp")

	if verificationErr := lc.verifyToolExistence(); verificationErr != nil {
		lc.logger.Error("Tool verification failed", map[string]interface{}{"error": verificationErr.Error()})
	}
}

func (lc *LaTeXCommand) findExecutableInPath(executableName string) string {
	path, lookupErr := exec.LookPath(executableName)
	if lookupErr != nil {
		lc.logger.Warn("Executable not found in PATH",
			map[string]interface{}{"executable": executableName})

		return executableName
	}

	return path
}

func (lc *LaTeXCommand) verifyToolExistence() error {
	toolVerifications := []struct {
		name         string
		path         string
		validationFn func(string) error
	}{
		{
			name:         "pdflatex",
			path:         lc.toolPaths.pdflatex,
			validationFn: validateAbsoluteExecutablePath,
		},
		{
			name:         "convert",
			path:         lc.toolPaths.convert,
			validationFn: validateAbsoluteExecutablePath,
		},
		{
			name:         "cwebp",
			path:         lc.toolPaths.cwebp,
			validationFn: validateAbsoluteExecutablePath,
		},
	}

	for _, tool := range toolVerifications {
		if validationErr := tool.validationFn(tool.path); validationErr != nil {
			return fmt.Errorf("%w: %s (%s)", ErrToolNotFound, tool.name, tool.path)
		}
	}

	return nil
}

func validateAbsoluteExecutablePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s", ErrPathNotAbsolute, path)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("path verification failed: %w", statErr)
	}

	return nil
}

func (lc *LaTeXCommand) createRenderContext() (*RenderContext, error) {
	tempDirectory, dirErr := os.MkdirTemp(lc.config.TempDir, "latex-")
	if dirErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrTempDirCreation, dirErr)
	}

	absoluteTempDir, absErr := filepath.Abs(tempDirectory)
	if absErr != nil {
		return nil, fmt.Errorf("absolute path conversion failed: %w", absErr)
	}

	renderContext := &RenderContext{
		tempDirectory: absoluteTempDir,
		filePaths:     make(map[string]string),
		logger:        lc.logger,
	}

	requiredFiles := []string{
		allowedBaseFilename + ".tex",
		allowedBaseFilename + ".pdf",
		allowedBaseFilename + ".png",
		allowedBaseFilename + ".webp",
	}

	for _, filename := range requiredFiles {
		if registerErr := renderContext.registerFilePath(filename); registerErr != nil {
			renderContext.cleanupResources()

			return nil, registerErr
		}
	}

	return renderContext, nil
}

func (renderCtx *RenderContext) registerFilePath(filename string) error {
	if !isAllowedFilename(filename) {
		return fmt.Errorf("%w: %s", ErrPathOutsideDir, filename)
	}

	fullPath := filepath.Join(renderCtx.tempDirectory, filename)
	if containmentErr := validatePathContainment(renderCtx.tempDirectory, fullPath); containmentErr != nil {
		return containmentErr
	}

	renderCtx.filePaths[filename] = fullPath

	return nil
}

func isAllowedFilename(filename string) bool {
	allowedExtensions := map[string]bool{
		".tex":  true,
		".pdf":  true,
		".png":  true,
		".webp": true,
	}
	base := strings.TrimSuffix(filename, filepath.Ext(filename))

	return base == allowedBaseFilename && allowedExtensions[filepath.Ext(filename)]
}

func validatePathContainment(baseDirectory, targetPath string) error {
	absoluteBase, baseErr := filepath.Abs(baseDirectory)
	if baseErr != nil {
		return fmt.Errorf("base directory error: %w", baseErr)
	}

	absoluteTarget, targetErr := filepath.Abs(targetPath)
	if targetErr != nil {
		return fmt.Errorf("target path error: %w", targetErr)
	}

	relativePath, relErr := filepath.Rel(absoluteBase, absoluteTarget)
	if relErr != nil {
		return fmt.Errorf("path relation error: %w", relErr)
	}

	if strings.HasPrefix(relativePath, "..") {
		return ErrPathOutsideDir
	}

	return nil
}

func (renderCtx *RenderContext) cleanupResources() {
	if removeErr := os.RemoveAll(renderCtx.tempDirectory); removeErr != nil && renderCtx.logger != nil {
		renderCtx.logger.Error("Temporary directory cleanup failed",
			map[string]interface{}{
				"directory": renderCtx.tempDirectory,
				"error":     removeErr.Error(),
			})
	}
}

func (lc *LaTeXCommand) executeSecuredCommand(
	ctx context.Context,
	commandName string,
	executablePath string,
	arguments ...string,
) error {
	if validationErr := validateAbsoluteExecutablePath(executablePath); validationErr != nil {
		return fmt.Errorf("command validation failed: %w", validationErr)
	}

	command := exec.CommandContext(ctx, executablePath, arguments...)
	startTime := time.Now()
	output, execErr := command.CombinedOutput()
	executionDuration := time.Since(startTime)

	logData := map[string]interface{}{
		"command":     command.String(),
		"duration_ms": executionDuration.Milliseconds(),
	}

	if execErr != nil {
		logData["output"] = string(output)
		logData["error"] = execErr.Error()
		lc.logger.Error(commandName+" failed", logData)

		return fmt.Errorf("%s execution failed: %w", commandName, execErr)
	}

	lc.logger.Debug(commandName+" completed", logData)

	return nil
}

func (lc *LaTeXCommand) renderLatex(ctx context.Context, latexCode string) ([]byte, error) {
	renderContext, ctxErr := lc.createRenderContext()
	if ctxErr != nil {
		return nil, ctxErr
	}
	defer renderContext.cleanupResources()

	if writeErr := lc.writeLatexContent(renderContext, latexCode); writeErr != nil {
		return nil, writeErr
	}

	processingSteps := []struct {
		name        string
		executionFn func(context.Context, *RenderContext) error
	}{
		{"PDFLaTeX Compilation", lc.executePDFLatex},
		{"PDF to PNG Conversion", lc.executeImageConversion},
		{"PNG to WebP Conversion", lc.executeWebPConversion},
	}

	for _, step := range processingSteps {
		if stepErr := step.executionFn(ctx, renderContext); stepErr != nil {
			return nil, fmt.Errorf("%s failed: %w", step.name, stepErr)
		}
	}

	return lc.readOutputFileSecurely(renderContext.filePaths[allowedBaseFilename+".webp"])
}

func (lc *LaTeXCommand) writeLatexContent(renderContext *RenderContext, code string) error {
	const latexTemplate = `\documentclass[preview]{standalone}
\usepackage{amsmath,amssymb,amsfonts}
\begin{document}
\thispagestyle{empty}
%s
\end{document}`

	content := fmt.Sprintf(latexTemplate, code)
	texFilePath := renderContext.filePaths[allowedBaseFilename+".tex"]

	if writeErr := os.WriteFile(texFilePath, []byte(content), secureFilePermissions); writeErr != nil {
		return fmt.Errorf("%w: %w", ErrWriteTexFile, writeErr)
	}

	return nil
}

func (lc *LaTeXCommand) executePDFLatex(ctx context.Context, renderContext *RenderContext) error {
	arguments := []string{
		"-no-shell-escape",
		"-interaction=nonstopmode",
		"-output-directory", renderContext.tempDirectory,
		renderContext.filePaths[allowedBaseFilename+".tex"],
	}

	return lc.executeSecuredCommand(
		ctx,
		"PDFLaTeX",
		lc.toolPaths.pdflatex,
		arguments...,
	)
}

func (lc *LaTeXCommand) executeImageConversion(ctx context.Context, renderContext *RenderContext) error {
	arguments := []string{
		"-density", "300",
		renderContext.filePaths[allowedBaseFilename+".pdf"],
		"-quality", "90",
		renderContext.filePaths[allowedBaseFilename+".png"],
	}

	return lc.executeSecuredCommand(
		ctx,
		"ImageMagick Convert",
		lc.toolPaths.convert,
		arguments...,
	)
}

func (lc *LaTeXCommand) executeWebPConversion(ctx context.Context, renderContext *RenderContext) error {
	arguments := []string{
		renderContext.filePaths[allowedBaseFilename+".png"],
		"-o", renderContext.filePaths[allowedBaseFilename+".webp"],
	}

	return lc.executeSecuredCommand(
		ctx,
		"CWebP Conversion",
		lc.toolPaths.cwebp,
		arguments...,
	)
}

func (lc *LaTeXCommand) readOutputFileSecurely(filePath string) ([]byte, error) {
	cleanPath := filepath.Clean(filePath)
	directory := filepath.Dir(cleanPath)

	if containmentErr := validatePathContainment(directory, cleanPath); containmentErr != nil {
		return nil, fmt.Errorf("output path validation failed: %w", containmentErr)
	}

	if fileInfo, statErr := os.Stat(cleanPath); statErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadOutputImage, statErr)
	} else if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrReadOutputImage)
	}

	content, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadOutputImage, readErr)
	}

	return content, nil
}

func (lc *LaTeXCommand) Name() string {
	return "latex"
}

func (lc *LaTeXCommand) Info() CommandInfo {
	return CommandInfo{
		Description: "Render LaTeX equations into WebP images",
		Usage:       "!latex <equation>",
		Examples: []string{
			"!latex x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}",
			"!latex \\int_{a}^{b} f(x)\\,dx = F(b) - F(a)",
		},
	}
}

func (lc *LaTeXCommand) Handle(ctx context.Context, message *message.Message) error {
	latexCode := strings.TrimSpace(strings.TrimPrefix(message.Text, "!latex"))
	if latexCode == "" {
		return ErrEmptyLatex
	}

	if len(latexCode) > maxLatexCodeLength {
		return ErrLatexTooLong
	}

	if validationErr := lc.validateLatexContent(latexCode); validationErr != nil {
		return validationErr
	}

	renderCtx, cancel := context.WithTimeout(ctx, lc.renderTimeout)
	defer cancel()

	webpImage, renderErr := lc.renderLatex(renderCtx, latexCode)
	if renderErr != nil {
		if errors.Is(renderCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w after %s", ErrRenderTimeout, lc.renderTimeout)
		}

		return fmt.Errorf("rendering failed: %w", renderErr)
	}

	if sendErr := lc.messageSender.SendImage(ctx, message.Recipient, webpImage, "LaTeX Render"); sendErr != nil {
		return fmt.Errorf("failed to send image: %w", sendErr)
	}

	return nil
}

func (lc *LaTeXCommand) validateLatexContent(code string) error {
	disallowedCommands := []string{"\\input", "\\include", "\\write18", "\\def", "\\let"}
	for _, cmd := range disallowedCommands {
		if strings.Contains(code, cmd) {
			return fmt.Errorf("%w: %s", ErrDisallowedLatexCmd, cmd)
		}
	}

	return nil
}
