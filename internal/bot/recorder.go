package bot

import (
	"context"
	"log/slog"
)

// Recorder receives the runtime's events by name, for metrics or tracing.
// Record is called from many goroutines and must not block.
//
// The names are message_received, message_dropped (reason), message_handled
// (duration, error when Handle failed), connected, disconnected and
// session_ended (reason, detail). Message events also carry id, chat and user.
type Recorder interface {
	Record(ctx context.Context, name string, attrs ...slog.Attr)
}

// logRecorder writes each event to the log at debug level.
type logRecorder struct {
	log *slog.Logger
}

func (r logRecorder) Record(ctx context.Context, name string, attrs ...slog.Attr) {
	r.log.LogAttrs(ctx, slog.LevelDebug, name, attrs...)
}
