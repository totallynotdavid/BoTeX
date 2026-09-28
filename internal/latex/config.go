package latex

import (
	"time"

	"github.com/totallynotdavid/botkit/internal/config"
)

// Environment keys the latex command reads. Byte sizes are plain integers.
const (
	KeyMaxLength    = "LATEX_MAX_LENGTH"
	KeyMaxImageSize = "LATEX_MAX_IMAGE_BYTES"
	KeyTimeout      = "LATEX_TIMEOUT"
	KeyDataLimit    = "LATEX_DATA_LIMIT_BYTES"
	KeyFileLimit    = "LATEX_FILE_SIZE_LIMIT_BYTES"
)

// ConfigFromEnv reads the command's limits, each defaulting to DefaultConfig's.
func ConfigFromEnv(env *config.Env) Config {
	def := DefaultConfig()

	limits := def.Limits
	limits.Timeout = env.Duration(KeyTimeout, def.Limits.Timeout, time.Millisecond)
	limits.Data = int64(env.Int(KeyDataLimit, int(def.Limits.Data), 1))
	limits.FileSize = int64(env.Int(KeyFileLimit, int(def.Limits.FileSize), 1))
	limits.MaxBytes = int64(env.Int(KeyMaxImageSize, int(def.Limits.MaxBytes), 1))

	return Config{
		MaxLength: env.Int(KeyMaxLength, def.MaxLength, 1),
		Limits:    limits,
	}
}
