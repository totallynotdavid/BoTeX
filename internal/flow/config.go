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

const requestsPerMinute = 20

// DefaultRateLimit is the bot's own default for the BOTKIT_RATE_LIMIT_* keys.
// A customer walking a menu sends several messages a minute, more than the
// shared default allows.
func DefaultRateLimit() config.RateLimit {
	return config.RateLimit{Requests: requestsPerMinute, Period: time.Minute, Cooldown: time.Minute}
}

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
