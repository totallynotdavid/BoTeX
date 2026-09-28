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
	KeyAllowOnly         = "BOTKIT_ALLOW_ONLY"
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
	// AllowOnly limits the senders the bot answers to these JIDs. Empty means
	// everyone.
	AllowOnly []string
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
// The JID lists are checked here so a malformed JID fails startup with the key
// named.
func (e *Env) Shared(def Shared) Shared {
	shared := Shared{
		Store:     e.String(KeyStore, def.Store),
		LogLevel:  e.Level(KeyLogLevel, def.LogLevel),
		Owners:    def.Owners,
		AllowOnly: def.AllowOnly,
		RateLimit: RateLimit{
			Requests: e.Int(KeyRateLimitRequests, def.RateLimit.Requests, 1),
			Period:   e.Duration(KeyRateLimitPeriod, def.RateLimit.Period, time.Millisecond),
			Cooldown: e.Duration(KeyRateLimitCooldown, def.RateLimit.Cooldown, 0),
		},
		MaxInFlight: e.Int(KeyMaxInFlight, def.MaxInFlight, 1),
		OwnMessages: e.Bool(KeyOwnMessages, def.OwnMessages),
	}

	e.jids(KeyOwners, &shared.Owners)
	e.jids(KeyAllowOnly, &shared.AllowOnly)

	return shared
}

// jids reads the JID list at key into list, which keeps its value when the key
// is unset or malformed.
func (e *Env) jids(key string, list *[]string) {
	raw, set := e.value(key)
	if !set {
		return
	}

	parsed, err := auth.ParseJIDs(raw)
	if err != nil {
		e.fail(key, raw, err)

		return
	}

	*list = parsed
}
