package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/totallynotdavid/botkit/pkg/config"
	"github.com/totallynotdavid/botkit/pkg/logger"
	"github.com/totallynotdavid/botkit/pkg/message"
	"github.com/totallynotdavid/botkit/pkg/timing"
)

const (
	defaultRenderTimeoutSec = 45
	maxLatexCodeLength      = 1000
	secureFilePermissions   = 0o600
	allowedBaseFilename     = "equation"

	// Resource bounds applied to every external process a render runs
	// (pdflatex, convert, cwebp), enforced via prlimit(1) so a single
	// malicious document cannot exhaust memory, CPU, or disk before the
	// renderTimeout wall-clock deadline fires.
	maxRenderCPUSeconds  = 20
	maxRenderMemoryBytes = 1024 * config.MB
	maxRenderOutputBytes = 64 * config.MB

	// maxLoggedOutputBytes bounds a resource RLIMIT_FSIZE cannot: the bot's
	// own memory. A tool's combined stdout+stderr is captured for logging
	// through a tailWriter instead of exec.Cmd.CombinedOutput, so a process
	// that only prints, rather than writes to a file, cannot grow that
	// capture without limit.
	maxLoggedOutputBytes = 64 * config.KB

	killedProcessWaitDelay = 2 * time.Second
)

var (
	ErrEmptyLatex         = errors.New("empty LaTeX equation")
	ErrLatexTooLong       = errors.New("LaTeX code exceeds character limit")
	ErrDisallowedLatexCmd = errors.New("disallowed LaTeX command")
	ErrTempDirCreation    = errors.New("temp dir creation failed")
	ErrWriteTexFile       = errors.New("failed to write tex file")
	ErrReadOutputImage    = errors.New("failed to read output image")
	ErrToolNotFound       = errors.New("required tool not found")
	ErrPathOutsideDir     = errors.New("file path is outside the permitted directory")
	ErrPathNotAbsolute    = errors.New("path is not absolute")
	ErrImageTooLarge      = errors.New("rendered image exceeds maximum allowed size")
	// ErrRenderLimitExceeded marks a render process stopped by a resource
	// bound (CPU, output size, or the wall-clock deadline) rather than
	// failing on its own, so a limit can be told apart from a LaTeX error.
	ErrRenderLimitExceeded = errors.New("render exceeded a resource limit")
)

// imageSender is the subset of *message.MessageSender that LaTeXCommand
// depends on, so tests can substitute a fake and assert on what was sent.
type imageSender interface {
	SendImage(ctx context.Context, recipient types.JID, imageData []byte, caption string) error
	SendText(ctx context.Context, recipient types.JID, text string) error
}

type LaTeXCommand struct {
	config        *config.Config
	messageSender imageSender
	logger        *logger.Logger
	timeTracker   *timing.Tracker
	renderTimeout time.Duration
	toolPaths     struct {
		pdflatex string
		convert  string
		cwebp    string
		prlimit  string
	}
}

type RenderContext struct {
	tempDirectory string
	filePaths     map[string]string
	logger        *logger.Logger
}

func NewLaTeXCommand(client *whatsmeow.Client, cfg *config.Config, timeTracker *timing.Tracker, loggerFactory *logger.Factory) *LaTeXCommand {
	command := &LaTeXCommand{
		config:        cfg,
		messageSender: message.NewMessageSender(client),
		logger:        loggerFactory.GetLogger("latex-command"),
		timeTracker:   timeTracker,
		renderTimeout: defaultRenderTimeoutSec * time.Second,
	}
	command.initializeToolPaths()

	return command
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

func (lc *LaTeXCommand) Handle(ctx context.Context, msg *message.Message) error {
	lc.logger.Info("LaTeX command received", map[string]any{
		"sender": msg.Sender,
		"text":   msg.Text,
	})

	lc.logger.Debug("Starting LaTeX command timing", map[string]any{
		"tracker": lc.timeTracker != nil,
	})

	err := lc.timeTracker.TrackCommand(ctx, "latex", func(ctx context.Context) error {
		return lc.handleLatexCommand(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to handle latex command: %w", err)
	}

	return nil
}

func (lc *LaTeXCommand) initializeToolPaths() {
	resolveToolPath := func(configPath, defaultExecutable string) string {
		if configPath != "" {
			absPath, err := filepath.Abs(configPath)
			if err != nil {
				lc.logger.Error("Absolute path resolution failed",
					map[string]any{"path": configPath, "error": err.Error()})

				return lc.findExecutableInPath(defaultExecutable)
			}

			return absPath
		}

		return lc.findExecutableInPath(defaultExecutable)
	}
	lc.toolPaths.pdflatex = resolveToolPath(lc.config.PDFLatexPath, "pdflatex")
	lc.toolPaths.convert = resolveToolPath(lc.config.ConvertPath, "convert")

	lc.toolPaths.cwebp = resolveToolPath(lc.config.CWebPPath, "cwebp")

	// prlimit(1) (util-linux) has no config override: it ships as part of
	// the base OS on every Linux host this bot targets, unlike the TeX Live
	// and ImageMagick tools that get installed to non-standard prefixes.
	lc.toolPaths.prlimit = lc.findExecutableInPath("prlimit")

	verificationErr := lc.verifyToolExistence()
	if verificationErr != nil {
		lc.logger.Error("Tool verification failed", map[string]any{"error": verificationErr.Error()})
	}
}

func (lc *LaTeXCommand) findExecutableInPath(executableName string) string {
	path, lookupErr := exec.LookPath(executableName)
	if lookupErr != nil {
		lc.logger.Warn("Executable not found in PATH",
			map[string]any{"executable": executableName})

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
		{
			name:         "prlimit",
			path:         lc.toolPaths.prlimit,
			validationFn: validateAbsoluteExecutablePath,
		},
	}
	for _, tool := range toolVerifications {
		validationErr := tool.validationFn(tool.path)
		if validationErr != nil {
			return fmt.Errorf("%w: %s (%s)", ErrToolNotFound, tool.name, tool.path)
		}
	}

	return nil
}

func validateAbsoluteExecutablePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s", ErrPathNotAbsolute, path)
	}

	_, statErr := os.Stat(path)
	if statErr != nil {
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
		registerErr := renderContext.registerFilePath(filename)
		if registerErr != nil {
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

	containmentErr := validatePathContainment(renderCtx.tempDirectory, fullPath)
	if containmentErr != nil {
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
	removeErr := os.RemoveAll(renderCtx.tempDirectory)
	if removeErr != nil && renderCtx.logger != nil {
		renderCtx.logger.Error("Temporary directory cleanup failed",
			map[string]any{
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
	return lc.executeSecuredCommandIn(ctx, "", commandName, executablePath, arguments...)
}

// executeSecuredCommandIn runs executablePath with workDir as its current
// directory (inherited from this process when workDir is empty). pdflatex
// needs this: under -cnf-line=openin_any=p it refuses to open its main .tex
// file at all when given as an absolute path, so it must be named relative
// to a working directory instead.
func (lc *LaTeXCommand) executeSecuredCommandIn(
	ctx context.Context,
	workDir string,
	commandName string,
	executablePath string,
	arguments ...string,
) error {
	_, _, err := lc.executeSecuredCommandInWithCapture(ctx, workDir, commandName, executablePath, arguments...)

	return err
}

// executeSecuredCommandInWithCapture is executeSecuredCommandIn's
// implementation, split out so tests can inspect the captured tail
// directly instead of only what ends up in a log file.
func (lc *LaTeXCommand) executeSecuredCommandInWithCapture(
	ctx context.Context,
	workDir string,
	commandName string,
	executablePath string,
	arguments ...string,
) (capturedOutput []byte, truncated bool, err error) {
	validationErr := validateAbsoluteExecutablePath(executablePath)
	if validationErr != nil {
		return nil, false, fmt.Errorf("command validation failed: %w", validationErr)
	}

	boundedArgs := append([]string{
		fmt.Sprintf("--cpu=%d", maxRenderCPUSeconds),
		fmt.Sprintf("--as=%d", maxRenderMemoryBytes),
		fmt.Sprintf("--fsize=%d", maxRenderOutputBytes),
		executablePath,
	}, arguments...)

	// prlimit execve()s executablePath after setting the limits above, so
	// the target process (already validated as an absolute path) inherits
	// them; no shell is involved.
	command := exec.CommandContext(ctx, lc.toolPaths.prlimit, boundedArgs...) // #nosec G204 -- executablePath is validated above
	command.Dir = workDir

	// A tool can spawn children (convert runs gs). Killing only the direct
	// process on deadline would leave a child holding the output pipe open, so
	// Wait would block until that child exits, defeating the deadline. Run
	// the tool in its own process group and kill the whole group; WaitDelay
	// is the backstop that closes the pipe if anything escapes the group.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = killedProcessWaitDelay

	tail := newTailWriter(maxLoggedOutputBytes)
	command.Stdout = tail
	command.Stderr = tail

	startTime := time.Now()
	execErr := command.Run()
	executionDuration := time.Since(startTime)

	capturedOutput, truncated = tail.Bytes()

	logData := map[string]any{
		"command":          command.String(),
		"duration_ms":      executionDuration.Milliseconds(),
		"output_truncated": truncated,
	}
	if execErr != nil {
		logData["output"] = string(capturedOutput)
		logData["error"] = execErr.Error()
		lc.logger.Error(commandName+" failed", logData)

		if stoppedByLimit(ctx, execErr) {
			return capturedOutput, truncated, fmt.Errorf("%s execution failed: %w: %w", commandName, ErrRenderLimitExceeded, execErr)
		}

		return capturedOutput, truncated, fmt.Errorf("%s execution failed: %w", commandName, execErr)
	}

	lc.logger.Debug(commandName+" completed", logData)

	return capturedOutput, truncated, nil
}

// stoppedByLimit reports whether a process ended because a resource bound
// stopped it, judged only from how it ended: the context deadline, or a
// fatal signal. prlimit's CPU limit kills with SIGKILL (soft and hard limits
// are equal), and an exceeded file-size limit raises SIGXFSZ. A process
// that exits on its own, including pdflatex reporting a LaTeX error with
// status 1, is never a limit stop, whatever it printed. A memory limit
// (RLIMIT_AS) makes allocations fail so the tool exits by itself; that is
// indistinguishable from any other failure and is not covered here.
func stoppedByLimit(ctx context.Context, execErr error) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}

	var exitErr *exec.ExitError
	if !errors.As(execErr, &exitErr) {
		return false
	}

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}

	signal := status.Signal()

	return signal == syscall.SIGKILL || signal == syscall.SIGXCPU || signal == syscall.SIGXFSZ
}

func (lc *LaTeXCommand) renderLatex(ctx context.Context, latexCode string) ([]byte, error) {
	renderContext, ctxErr := lc.createRenderContext()
	if ctxErr != nil {
		return nil, ctxErr
	}
	defer renderContext.cleanupResources()

	writeErr := lc.writeLatexContent(renderContext, latexCode)
	if writeErr != nil {
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
		stepErr := step.executionFn(ctx, renderContext)
		if stepErr != nil {
			return nil, fmt.Errorf("%s failed: %w", step.name, stepErr)
		}
	}

	return lc.readOutputFileSecurely(renderContext.filePaths[allowedBaseFilename+".webp"])
}

func (lc *LaTeXCommand) writeLatexContent(renderContext *RenderContext, code string) error {
	const latexTemplate = `\documentclass[preview]{standalone}
\usepackage{amsmath,amssymb,amsfonts,physics}
\begin{document}
\thispagestyle{empty}
\begin{align*}
%s
\end{align*}
\end{document}`

	content := fmt.Sprintf(latexTemplate, code)

	texFilePath := renderContext.filePaths[allowedBaseFilename+".tex"]

	writeErr := os.WriteFile(texFilePath, []byte(content), secureFilePermissions)
	if writeErr != nil {
		return fmt.Errorf("%w: %w", ErrWriteTexFile, writeErr)
	}

	return nil
}

func (lc *LaTeXCommand) executePDFLatex(ctx context.Context, renderContext *RenderContext) error {
	arguments := []string{
		"-no-shell-escape",
		// Paranoid kpathsea file access: pdflatex may only open input files
		// named relative to its working directory (openin_any=p rejects an
		// absolute path outright, including its own main .tex argument,
		// which is why it is passed by base name below with Dir set to the
		// render's temp directory) and may only write output files there
		// too, refusing paths that escape via ".." (openout_any=p).
		"-cnf-line=openin_any=p",
		"-cnf-line=openout_any=p",
		"-interaction=nonstopmode",
		"-output-directory", renderContext.tempDirectory,
		allowedBaseFilename + ".tex",
	}

	return lc.executeSecuredCommandIn(
		ctx,
		renderContext.tempDirectory,
		"PDFLaTeX",
		lc.toolPaths.pdflatex,
		arguments...,
	)
}

func (lc *LaTeXCommand) executeImageConversion(ctx context.Context, renderContext *RenderContext) error {
	arguments := []string{
		"-density", "300",
		"-trim",
		"-background", "white",
		"-alpha", "remove",
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

	containmentErr := validatePathContainment(directory, cleanPath)
	if containmentErr != nil {
		return nil, fmt.Errorf("output path validation failed: %w", containmentErr)
	}

	fileInfo, statErr := os.Stat(cleanPath)
	if statErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadOutputImage, statErr)
	}

	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrReadOutputImage)
	}

	content, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadOutputImage, readErr)
	}

	return content, nil
}

func (lc *LaTeXCommand) handleLatexCommand(ctx context.Context, msg *message.Message) error {
	latexCode := strings.TrimSpace(strings.TrimPrefix(msg.Text, "!latex"))

	err := lc.validateLatexInput(latexCode)
	if err != nil {
		return err
	}

	return lc.renderAndSendLatex(ctx, latexCode, msg)
}

func (lc *LaTeXCommand) validateLatexInput(latexCode string) error {
	if latexCode == "" {
		return ErrEmptyLatex
	}

	if len(latexCode) > maxLatexCodeLength {
		return ErrLatexTooLong
	}

	validationErr := lc.validateLatexContent(latexCode)
	if validationErr != nil {
		return validationErr
	}

	return nil
}

func (lc *LaTeXCommand) renderAndSendLatex(ctx context.Context, latexCode string, msg *message.Message) error {
	var webpImage []byte

	// The wall-clock deadline covers only the render. The user notice and the
	// image send below run on the caller's context, which stays live after
	// the deadline fires.
	renderErr := lc.timeTracker.TrackSubOperation(ctx, "latex_render", func(ctx context.Context) error {
		renderCtx, cancel := context.WithTimeout(ctx, lc.renderTimeout)
		defer cancel()

		var err error

		webpImage, err = lc.renderLatex(renderCtx, latexCode)

		return err
	})
	if renderErr != nil {
		if errors.Is(renderErr, ErrRenderLimitExceeded) {
			lc.logger.Warn("Render stopped by a resource limit", map[string]any{
				"sender": msg.Sender,
				"error":  renderErr.Error(),
			})

			textErr := lc.messageSender.SendText(ctx, msg.Recipient,
				"That equation took too long or used too many resources to render. Try a shorter or simpler expression.")
			if textErr != nil {
				return fmt.Errorf("failed to send render limit message: %w", textErr)
			}
		}

		return fmt.Errorf("failed to render latex: %w", renderErr)
	}

	return lc.deliverImage(ctx, webpImage, msg)
}

// deliverImage refuses to send an image larger than config.MaxImageSize and
// tells the user instead, so the configured cap holds whatever the render
// produced.
func (lc *LaTeXCommand) deliverImage(ctx context.Context, webpImage []byte, msg *message.Message) error {
	imageSize := int64(len(webpImage))
	if imageSize > lc.config.MaxImageSize {
		lc.logger.Warn("Rendered image exceeds configured size limit", map[string]any{
			"sender":         msg.Sender,
			"size_bytes":     imageSize,
			"max_size_bytes": lc.config.MaxImageSize,
		})

		textErr := lc.messageSender.SendText(ctx, msg.Recipient,
			"The rendered equation is too large to send. Try a shorter or simpler expression.")
		if textErr != nil {
			return fmt.Errorf("failed to send image size limit message: %w", textErr)
		}

		return fmt.Errorf("%w: %d bytes exceeds limit of %d bytes", ErrImageTooLarge, imageSize, lc.config.MaxImageSize)
	}

	err := lc.messageSender.SendImage(ctx, msg.Recipient, webpImage, "LaTeX Render")
	if err != nil {
		return fmt.Errorf("failed to send latex image: %w", err)
	}

	return nil
}

// validateLatexContent blocks the direct spellings of file and shell access
// commands. It is not a resource bound and cannot be one: TeX macros can
// rebuild any of these at runtime (\csname, \catcode, \newcommand,
// \expandafter), so a denylist only stops the laziest attempts. Memory, CPU
// time, and output size are bounded for every external process a render
// runs via prlimit in executeSecuredCommandInWithCapture, and pdflatex's own file access
// is restricted via openin_any/openout_any in executePDFLatex.
func (lc *LaTeXCommand) validateLatexContent(code string) error {
	disallowedCommands := []string{"\\input", "\\include", "\\write18"}
	for _, cmd := range disallowedCommands {
		if strings.Contains(code, cmd) {
			return fmt.Errorf("%w: %s", ErrDisallowedLatexCmd, cmd)
		}
	}

	return nil
}
