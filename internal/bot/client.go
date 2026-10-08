package bot

import (
	"context"
	"errors"
)

// ErrAlreadyConnected is returned by Connect when an earlier Connect has not
// been ended by Disconnect.
var ErrAlreadyConnected = errors.New("already connected")

// Transport is the boundary between the bot runtime and a messaging service.
// internal/whatsapp implements it over whatsmeow and internal/whatsapp/fake
// implements it in memory for tests and the offline REPL.
type Transport interface {
	// Connect starts the session and reports every event to handle until
	// Disconnect. ctx bounds only the connection attempt; the session runs
	// until Disconnect. It returns a *SessionEndedError with Reason NotPaired
	// when the store holds no device, without contacting WhatsApp, and
	// ErrAlreadyConnected while a session is running. handle runs on the
	// connection's goroutine, so a slow handle delays every later event.
	Connect(ctx context.Context, handle func(Event)) error
	Disconnect()
	// OwnJID is the paired account's phone-number JID, or "" before pairing.
	OwnJID() JID
	SendText(ctx context.Context, recipient JID, text string) error
	SendImage(ctx context.Context, recipient JID, img Image) error
	// React sets emoji as the bot's reaction to msg. An empty emoji removes it.
	React(ctx context.Context, msg Message, emoji string) error
	Download(ctx context.Context, media *Media) ([]byte, error)
}

// WorkTracker is an optional transport hook for deterministic clients. The
// runtime calls it around accepted message work so an offline runner can tell
// that a silent handler has finished without guessing from elapsed time.
type WorkTracker interface {
	BeginWork()
	EndWork()
}
