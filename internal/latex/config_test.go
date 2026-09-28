package latex_test

import (
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/config"
	"github.com/totallynotdavid/botkit/internal/latex"
)

func envOf(vars map[string]string) *config.Env {
	return config.New(func(key string) (string, bool) {
		raw, ok := vars[key]

		return raw, ok
	})
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Parallel()

	env := envOf(nil)

	if got, want := latex.ConfigFromEnv(env), latex.DefaultConfig(); got != want {
		t.Errorf("ConfigFromEnv() = %+v, want %+v", got, want)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	t.Parallel()

	env := envOf(map[string]string{
		latex.KeyMaxLength:    "200",
		latex.KeyMaxImageSize: "1000000",
		latex.KeyTimeout:      "3s",
		latex.KeyDataLimit:    "134217728",
		latex.KeyFileLimit:    "8388608",
	})

	got := latex.ConfigFromEnv(env)

	if got.MaxLength != 200 {
		t.Errorf("MaxLength = %d, want 200", got.MaxLength)
	}

	if got.Limits.MaxBytes != 1_000_000 {
		t.Errorf("Limits.MaxBytes = %d, want 1000000", got.Limits.MaxBytes)
	}

	if got.Limits.Timeout != 3*time.Second {
		t.Errorf("Limits.Timeout = %v, want 3s", got.Limits.Timeout)
	}

	if got.Limits.Data != 134_217_728 || got.Limits.FileSize != 8_388_608 {
		t.Errorf("Limits.Data = %d and FileSize = %d, want 134217728 and 8388608", got.Limits.Data, got.Limits.FileSize)
	}

	if got.Limits.MaxPixels != latex.DefaultConfig().Limits.MaxPixels {
		t.Errorf("Limits.MaxPixels = %d, want the default", got.Limits.MaxPixels)
	}

	err := env.Err()
	if err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestConfigFromEnvBadValuesNameTheirKey(t *testing.T) {
	t.Parallel()

	for key, value := range map[string]string{
		latex.KeyMaxLength:    "long",
		latex.KeyMaxImageSize: "0",
		latex.KeyTimeout:      "-1s",
		latex.KeyDataLimit:    "lots",
		latex.KeyFileLimit:    "-5",
	} {
		env := envOf(map[string]string{key: value})
		latex.ConfigFromEnv(env)

		err := env.Err()
		if err == nil || !strings.Contains(err.Error(), key+"=") {
			t.Errorf("%s=%q: Err() = %v, want an error naming the key", key, value, err)
		}
	}
}
