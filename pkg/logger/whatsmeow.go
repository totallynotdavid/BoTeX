package logger

import (
	"fmt"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type WhatsmeowLogger struct {
	logger *Logger
	tag    string
}

func NewWhatsmeowLogger(logger *Logger, tag string) *WhatsmeowLogger {
	return &WhatsmeowLogger{
		logger: logger,
		tag:    tag,
	}
}

func (w *WhatsmeowLogger) Errorf(msg string, args ...any) {
	w.logger.Errorf(msg, args...)
}

func (w *WhatsmeowLogger) Warnf(msg string, args ...any) {
	w.logger.Warnf(msg, args...)
}

func (w *WhatsmeowLogger) Infof(msg string, args ...any) {
	w.logger.Infof(msg, args...)
}

func (w *WhatsmeowLogger) Debugf(msg string, args ...any) {
	w.logger.Debugf(msg, args...)
}

// Sub must have this signature to satisfy waLog.Logger interface.
// We intentionally return the interface here (value is a *WhatsmeowLogger
// which is assignable to waLog.Logger)
//
//nolint:ireturn // returns interface to satisfy waLog.Logger.Sub signature
func (w *WhatsmeowLogger) Sub(module string) waLog.Logger {
	subTag := fmt.Sprintf("%s/%s", w.tag, module)

	return NewWhatsmeowLogger(w.logger, subTag)
}
