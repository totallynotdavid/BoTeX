package whatsapp

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// waLogger writes whatsmeow's printf-style logs to slog, tagging each record
// with the whatsmeow module that produced it ("Client/Socket").
type waLogger struct {
	log    *slog.Logger
	module string
}

func (w waLogger) Errorf(msg string, args ...any) { w.logf(slog.LevelError, msg, args) }
func (w waLogger) Warnf(msg string, args ...any)  { w.logf(slog.LevelWarn, msg, args) }
func (w waLogger) Infof(msg string, args ...any)  { w.logf(slog.LevelInfo, msg, args) }
func (w waLogger) Debugf(msg string, args ...any) { w.logf(slog.LevelDebug, msg, args) }

//nolint:ireturn // waLog.Logger's Sub is declared to return the interface.
func (w waLogger) Sub(module string) waLog.Logger {
	return waLogger{log: w.log, module: w.module + "/" + module}
}

func (w waLogger) logf(level slog.Level, msg string, args []any) {
	ctx := context.Background()
	if !w.log.Enabled(ctx, level) {
		return
	}

	w.log.Log(ctx, level, fmt.Sprintf(msg, args...), slog.String("module", w.module))
}
