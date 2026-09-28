// Package flow runs a conversation flow: it keeps what each user has told the
// bot, applies the actions the flow's transitions name, and renders the
// messages the flow sends.
package flow

import (
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// State is what the bot knows about one user. A user who has never written has
// the zero State with only UserID set, so CurrentNode is empty until the flow
// places them.
type State struct {
	UserID             bot.JID
	CurrentNode        string
	UserName           string
	CourseInterest     string
	SelectedCourseID   string
	ConsultedPrice     bool
	VoucherPath        string
	RequiresHumanAgent bool
	RepromptCount      int
	// LastUpdated is when the store last saved the state.
	LastUpdated time.Time
}

// Direction says who sent a stored message.
type Direction string

const (
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

// StoredMessage is one line of a user's conversation history.
type StoredMessage struct {
	Timestamp time.Time
	Direction Direction
	Content   string
	// NodeID is the node the user was in when the message was sent or received.
	NodeID string
}
