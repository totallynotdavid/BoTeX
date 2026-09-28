package config

import (
	"log/slog"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
)

// Environment keys every bot reads.
const (
	KeyStore             = "BOTKIT_STORE_PATH"
	KeyLogLevel          = "BOTKIT_LOG_LEVEL"
	KeyOwners            = "BOTKIT_OWNER_JIDS"
	KeyRateLimitRequests = "BOTKIT_RATE_LIMIT_REQUESTS"
	KeyRateLimitPeriod   = "BOTKIT_RATE_LIMIT_PERIOD"
	KeyRateLimitCooldown = "BOTKIT_RATE_LIMIT_COOLDOWN"
	KeyMaxInFlight       = "BOTKIT_MAX_IN_FLIGHT"
	KeyOwnMessages       = "BOTKIT_OWN_MESSAGES"
)

// RateLimit is how many requests each user gets per period, and how long the
// bot waits before telling a user over the limit again.
type RateLimit struct {
	Requests int
	Period   time.Duration
	Cooldown time.Duration
}

// Shared is the configuration every bot has.
type Shared struct {
	// Store is the path of the SQLite file holding the WhatsApp session and
	// the bot's data.
	Store       string
	LogLevel    slog.Level
	Owners      []string
	RateLimit   RateLimit
	MaxInFlight int
	// OwnMessages makes the bot answer messages sent from its own account.
	OwnMessages bool
}

const (
	defaultRequests    = 5
	defaultCooldown    = 5 * time.Minute
	defaultMaxInFlight = 10
)

// DefaultShared returns the settings of a bot that sets nothing.
func DefaultShared() Shared {
	return Shared{
		Store:    "botkit.db",
		LogLevel: slog.LevelInfo,
		RateLimit: RateLimit{
			Requests: defaultRequests,
			Period:   time.Minute,
			Cooldown: defaultCooldown,
		},
		MaxInFlight: defaultMaxInFlight,
	}
}

// Shared reads the shared settings, each falling back to its value in def.
// The owner list is checked here so a malformed JID fails startup with the
// key named.
func (e *Env) Shared(def Shared) Shared {
	shared := Shared{
		Store:    e.String(KeyStore, def.Store),
		LogLevel: e.Level(KeyLogLevel, def.LogLevel),
		Owners:   def.Owners,
		RateLimit: RateLimit{
			Requests: e.Int(KeyRateLimitRequests, def.RateLimit.Requests, 1),
			Period:   e.Duration(KeyRateLimitPeriod, def.RateLimit.Period, time.Millisecond),
			Cooldown: e.Duration(KeyRateLimitCooldown, def.RateLimit.Cooldown, 0),
		},
		MaxInFlight: e.Int(KeyMaxInFlight, def.MaxInFlight, 1),
		OwnMessages: e.Bool(KeyOwnMessages, def.OwnMessages),
	}

	raw, set := e.value(KeyOwners)
	if set {
		owners, err := auth.ParseOwners(raw)
		if err != nil {
			e.fail(KeyOwners, raw, err)
		} else {
			shared.Owners = owners
		}
	}

	return shared
}
