package bot

import "fmt"

// Event is something a Transport reports to the runtime. The set is closed:
// MessageReceived, Connected, Disconnected and SessionEnded.
type Event interface {
	event()
}

// MessageReceived carries a message for the app.
type MessageReceived struct {
	Message Message
}

// Connected reports that the session is authenticated and receiving.
type Connected struct{}

// Disconnected reports a dropped connection that the Transport retries on its
// own.
type Disconnected struct{}

// SessionEnded reports that the session is gone and will not come back without
// the operator: the device must be paired again, or another process must stop.
type SessionEnded struct {
	Reason SessionEndReason
	// Detail is the server's reason code or ban expiry, for the operator.
	Detail string
}

func (MessageReceived) event() {}
func (Connected) event()       {}
func (Disconnected) event()    {}
func (SessionEnded) event()    {}

// SessionEndReason says why a session ended.
type SessionEndReason int

const (
	// NotPaired means the store holds no device.
	NotPaired SessionEndReason = iota + 1
	// LoggedOut means the device was unlinked from the phone or by WhatsApp.
	// The store has already deleted it.
	LoggedOut
	// Replaced means another process connected with the same device keys.
	Replaced
	// Banned means WhatsApp temporarily banned the account.
	Banned
	// Outdated means WhatsApp rejected the client version.
	Outdated
	// Refused means WhatsApp rejected the connection for another reason and
	// the client will not retry.
	Refused
)

func (r SessionEndReason) String() string {
	switch r {
	case NotPaired:
		return "not_paired"
	case LoggedOut:
		return "logged_out"
	case Replaced:
		return "replaced"
	case Banned:
		return "banned"
	case Outdated:
		return "outdated"
	case Refused:
		return "refused"
	default:
		return fmt.Sprintf("SessionEndReason(%d)", int(r))
	}
}

// SessionEndedError is returned when work stops because the session ended.
type SessionEndedError struct {
	Reason SessionEndReason
	Detail string
}

func (e *SessionEndedError) Error() string {
	if e.Detail == "" {
		return "whatsapp session ended: " + e.Reason.String()
	}

	return fmt.Sprintf("whatsapp session ended: %s (%s)", e.Reason, e.Detail)
}
