// Package bot is the runtime both bots share. It defines the messages and
// events an app sees and the Client it needs from a WhatsApp connection, so
// apps and their tests never depend on whatsmeow.
package bot

import "time"

// JID is a WhatsApp address such as "51999999999@s.whatsapp.net" or
// "120363000000000000@g.us", without a device part.
type JID string

// Message is an incoming message that carries text, media, or both.
type Message struct {
	ID string
	// Chat is where replies go: the group, or the other party of a direct chat.
	Chat JID
	// Sender is the author's address as WhatsApp delivered it, which may be a
	// LID ("...@lid"). Reactions need it.
	Sender JID
	// User is the author's phone-number JID when WhatsApp disclosed it, and
	// Sender otherwise. Users, ranks and rate limits key on it.
	User     JID
	PushName string
	Group    bool
	// FromMe is set for messages sent by the bot's own account, for example
	// typed on the paired phone.
	FromMe bool
	// Text is the message body or the media caption.
	Text  string
	Media *Media
	Time  time.Time
}

// MediaKind names the kind of attachment a message carries.
type MediaKind string

const (
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaDocument MediaKind = "document"
	MediaSticker  MediaKind = "sticker"
)

// Media describes an attachment. Its bytes are fetched on demand with
// Client.Download.
type Media struct {
	Kind MediaKind
	MIME string
	// Raw belongs to the Client that produced the message. Pass the Media
	// back to that Client's Download; other code must not read it.
	Raw any
}

// Image is an image to send.
type Image struct {
	Data    []byte
	MIME    string
	Caption string
}
