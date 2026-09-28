package flow

import (
	"time"

	"github.com/totallynotdavid/botkit/internal/config"
)

// Environment keys the flow bot reads.
const (
	KeyFile        = "FLOW_FILE"
	KeyVoucherDir  = "FLOW_VOUCHER_DIR"
	KeyTypingDelay = "FLOW_TYPING_DELAY"
)

// DefaultVoucherDir is where vouchers go when FLOW_VOUCHER_DIR is not set,
// relative to the working directory.
const DefaultVoucherDir = "vouchers"

// Config is what the flow bot reads from its environment.
type Config struct {
	// File is the flow file. Empty means the flow built into the binary.
	File string
	// VoucherDir is where payment vouchers are saved.
	VoucherDir string
	// TypingDelay is how long the bot waits before each reply, so answers do
	// not land the instant the user writes. Zero sends at once.
	TypingDelay time.Duration
}

// ConfigFromEnv reads the bot's settings. The keys that shape the runtime, the
// database and the log are shared with the other bots and read elsewhere.
func ConfigFromEnv(env *config.Env) Config {
	return Config{
		File:        env.String(KeyFile, ""),
		VoucherDir:  env.String(KeyVoucherDir, DefaultVoucherDir),
		TypingDelay: env.Duration(KeyTypingDelay, 0, 0),
	}
}
