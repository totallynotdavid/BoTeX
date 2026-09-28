package cli

import (
	"context"
	"errors"

	"github.com/totallynotdavid/botkit/internal/bot"
)

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	// ExitConfig is EX_CONFIG from sysexits.h. The bot returns it when the
	// WhatsApp session is gone and only the operator can fix that, so a service
	// manager told to skip restarts on it does not loop.
	ExitConfig = 78
)

// ExitStatus is the process exit status for the error run returned: 78 when the
// WhatsApp session ended, 0 when ctx was cancelled to stop the bot, 1
// otherwise.
func ExitStatus(ctx context.Context, err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, new(*bot.SessionEndedError)):
		return ExitConfig
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return ExitOK
	default:
		return ExitFailure
	}
}
