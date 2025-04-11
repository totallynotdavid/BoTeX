package message

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Message struct {
	Text      string
	Sender    types.JID
	IsGroup   bool
	GroupID   types.JID
	Recipient types.JID
	MessageID string

	// Message content
	ImageMessage    *waE2E.ImageMessage
	DocumentMessage *waE2E.DocumentMessage
	ExtendedText    *waE2E.ExtendedTextMessage

	RawMessage *waE2E.Message
}

func NewMessage(evt *events.Message) *Message {
	msg := &Message{
		Sender:     evt.Info.Sender,
		IsGroup:    evt.Info.IsGroup,
		RawMessage: evt.Message,
		MessageID:  evt.Info.ID,
	}

	if msg.IsGroup {
		groupID, err := types.ParseJID(evt.Info.Chat.String())
		if err == nil {
			msg.GroupID = groupID
			msg.Recipient = groupID
		}
	} else {
		msg.Recipient = evt.Info.Sender
	}

	// Extract message content
	if evt.Message.GetConversation() != "" {
		msg.Text = evt.Message.GetConversation()
	} else {
		msg.ImageMessage = evt.Message.GetImageMessage()
		msg.DocumentMessage = evt.Message.GetDocumentMessage()
		msg.ExtendedText = evt.Message.GetExtendedTextMessage()

		if msg.ExtendedText != nil {
			msg.Text = msg.ExtendedText.GetText()
		}
	}

	return msg
}

func (m *Message) GetText() string {
	if m.Text != "" {
		return m.Text
	}
	if m.ExtendedText != nil {
		return m.ExtendedText.GetText()
	}
	if m.ImageMessage != nil {
		return m.ImageMessage.GetCaption()
	}
	if m.DocumentMessage != nil {
		return m.DocumentMessage.GetCaption()
	}
	return ""
}
