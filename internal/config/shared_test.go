package config_test

import (
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/auth"
	"github.com/totallynotdavid/botkit/internal/config"
)

func TestSharedDefaults(t *testing.T) {
	t.Parallel()

	env := fromMap(nil)
	got := env.Shared(config.DefaultShared())

	want := config.Shared{
		Store:       "botkit.db",
		LogLevel:    slog.LevelInfo,
		RateLimit:   config.RateLimit{Requests: 5, Period: time.Minute, Cooldown: 5 * time.Minute},
		MaxInFlight: 10,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Shared() = %#v, want %#v", got, want)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestSharedOverridesEachType(t *testing.T) {
	t.Parallel()

	env := fromMap(map[string]string{
		config.KeyStore:             "/var/lib/bot/state.db",
		config.KeyLogLevel:          "debug",
		config.KeyOwners:            "51900000001@s.whatsapp.net, 51900000002@s.whatsapp.net",
		config.KeyRateLimitRequests: "20",
		config.KeyRateLimitPeriod:   "30s",
		config.KeyRateLimitCooldown: "2m",
		config.KeyMaxInFlight:       "3",
		config.KeyOwnMessages:       "true",
	})

	got := env.Shared(config.DefaultShared())

	want := config.Shared{
		Store:       "/var/lib/bot/state.db",
		LogLevel:    slog.LevelDebug,
		Owners:      []string{"51900000001@s.whatsapp.net", "51900000002@s.whatsapp.net"},
		RateLimit:   config.RateLimit{Requests: 20, Period: 30 * time.Second, Cooldown: 2 * time.Minute},
		MaxInFlight: 3,
		OwnMessages: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Shared() = %#v, want %#v", got, want)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestSharedKeepsCallerDefaults(t *testing.T) {
	t.Parallel()

	def := config.DefaultShared()
	def.RateLimit.Requests = 60

	got := fromMap(map[string]string{config.KeyMaxInFlight: "2"}).Shared(def)

	if got.RateLimit.Requests != 60 || got.MaxInFlight != 2 {
		t.Errorf("Shared() requests = %d, in flight = %d, want 60 and 2", got.RateLimit.Requests, got.MaxInFlight)
	}
}

func TestSharedBadValuesNameTheirKey(t *testing.T) {
	t.Parallel()

	for key, value := range map[string]string{
		config.KeyLogLevel:          "loud",
		config.KeyRateLimitRequests: "0",
		config.KeyRateLimitPeriod:   "soon",
		config.KeyMaxInFlight:       "many",
		config.KeyOwnMessages:       "maybe",
		config.KeyOwners:            "not-a-jid",
	} {
		env := fromMap(map[string]string{key: value})
		env.Shared(config.DefaultShared())

		err := env.Err()
		if err == nil || !strings.Contains(err.Error(), key+"=") {
			t.Errorf("%s=%q: Err() = %v, want an error naming the key", key, value, err)
		}
	}
}

func TestSharedBadOwnerWrapsTheParseError(t *testing.T) {
	t.Parallel()

	env := fromMap(map[string]string{config.KeyOwners: "user@server:3"})
	env.Shared(config.DefaultShared())

	err := env.Err()
	if !errors.Is(err, auth.ErrInvalidOwnerJID) {
		t.Errorf("Err() = %v, want auth.ErrInvalidOwnerJID", err)
	}
}
