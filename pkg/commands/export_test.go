package commands

import (
	"context"
	"time"

	"github.com/totallynotdavid/botkit/pkg/config"
	"github.com/totallynotdavid/botkit/pkg/logger"
	"github.com/totallynotdavid/botkit/pkg/message"
	"github.com/totallynotdavid/botkit/pkg/timing"
)

// Exported seams for the black-box tests in latex_test.go (package
// commands_test) to reach LaTeXCommand's unexported render pipeline,
// denylist and delivery logic without duplicating them. This file only
// exists for _test.go builds, so none of it ships in the production binary.

// NewTestLaTeXCommand builds a LaTeXCommand wired to the given fake sender,
// with its tool paths resolved from PATH exactly as production does.
func NewTestLaTeXCommand(cfg *config.Config, sender imageSender, log *logger.Logger) *LaTeXCommand {
	command := &LaTeXCommand{
		config:        cfg,
		messageSender: sender,
		logger:        log,
		timeTracker:   timing.NewTracker(timing.Config{Level: timing.Disabled}, log),
		renderTimeout: defaultRenderTimeoutSec * time.Second,
	}
	command.initializeToolPaths()

	return command
}

func (lc *LaTeXCommand) RenderTimeout() time.Duration { return lc.renderTimeout }

func (lc *LaTeXCommand) MaxRenderCPUSeconds() int { return maxRenderCPUSeconds }

func (lc *LaTeXCommand) RenderLatex(ctx context.Context, code string) ([]byte, error) {
	return lc.renderLatex(ctx, code)
}

func (lc *LaTeXCommand) RenderAndSendLatex(ctx context.Context, code string, msg *message.Message) error {
	return lc.renderAndSendLatex(ctx, code, msg)
}

func (lc *LaTeXCommand) ValidateLatexContent(code string) error {
	return lc.validateLatexContent(code)
}

func (lc *LaTeXCommand) DeliverImage(ctx context.Context, webpImage []byte, msg *message.Message) error {
	return lc.deliverImage(ctx, webpImage, msg)
}

// ExecuteSecuredCommandCapture runs a command exactly as executeSecuredCommandIn
// does (prlimit bounds, tail-capped output capture), but also returns the
// captured tail so tests can assert on it directly instead of scraping logs.
func (lc *LaTeXCommand) ExecuteSecuredCommandCapture(
	ctx context.Context,
	workDir string,
	commandName string,
	executablePath string,
	arguments ...string,
) (capturedOutput []byte, truncated bool, err error) {
	return lc.executeSecuredCommandInWithCapture(ctx, workDir, commandName, executablePath, arguments...)
}

func MaxLoggedOutputBytes() int { return maxLoggedOutputBytes }

func DenyReason(ctx context.Context, authService authorizer, userID, groupID, command string) (string, error) {
	handler := &CommandHandler{authService: authService}

	return handler.denyReason(ctx, userID, groupID, command)
}

// NewTailWriterForTest returns *tailWriter, an unexported type whose Write
// and Bytes methods are already exported (Write to satisfy io.Writer); a
// commands_test caller can use the returned value without ever naming the
// type.
func NewTailWriterForTest(maxBytes int) *tailWriter { return newTailWriter(maxBytes) }
