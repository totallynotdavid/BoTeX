package whatsapp

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// translate maps a whatsmeow event to a runtime event. It reports false for
// events the runtime has no use for.
//
//nolint:ireturn // bot.Event is the sealed set every whatsmeow event maps into.
func translate(evt any) (bot.Event, bool) {
	switch typed := evt.(type) {
	case *events.Message:
		msg, ok := toMessage(typed)
		if !ok {
			return nil, false
		}

		return bot.MessageReceived{Message: msg}, true
	case *events.Connected:
		return bot.Connected{}, true
	case *events.Disconnected:
		return bot.Disconnected{}, true
	case events.PermanentDisconnect:
		return sessionEnded(typed), true
	default:
		return nil, false
	}
}

// sessionEnded names the reason for each event after which whatsmeow stops
// reconnecting. Those without a known remedy are reported as refused.
func sessionEnded(evt events.PermanentDisconnect) bot.SessionEnded {
	switch typed := evt.(type) {
	case *events.LoggedOut:
		return bot.SessionEnded{Reason: bot.LoggedOut, Detail: typed.Reason.String()}
	case *events.StreamReplaced:
		return bot.SessionEnded{Reason: bot.Replaced}
	case *events.TemporaryBan:
		return bot.SessionEnded{Reason: bot.Banned, Detail: typed.String()}
	case *events.ClientOutdated:
		return bot.SessionEnded{Reason: bot.Outdated}
	default:
		return bot.SessionEnded{Reason: bot.Refused, Detail: evt.PermanentDisconnectDescription()}
	}
}

// toMessage keeps messages from direct chats and groups that carry text or
// media. Status updates, broadcast lists, newsletters, reactions and protocol
// messages are dropped.
func toMessage(evt *events.Message) (bot.Message, bool) {
	info := evt.Info

	switch info.Chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
	default:
		return bot.Message{}, false
	}

	text, media := content(evt.Message)
	if strings.TrimSpace(text) == "" && media == nil {
		return bot.Message{}, false
	}

	return bot.Message{
		ID:       info.ID,
		Chat:     bot.JID(info.Chat.ToNonAD().String()),
		Sender:   bot.JID(info.Sender.ToNonAD().String()),
		User:     bot.JID(phoneJID(info.Sender, info.SenderAlt).String()),
		PushName: info.PushName,
		Group:    info.IsGroup,
		FromMe:   info.IsFromMe,
		Text:     text,
		Media:    media,
		Time:     info.Timestamp,
	}, true
}

// phoneJID prefers the phone-number address of a sender that WhatsApp
// addressed by LID, because owners and users are configured by phone number.
func phoneJID(sender, alt types.JID) types.JID {
	if sender.Server == types.HiddenUserServer && alt.Server == types.DefaultUserServer {
		return alt.ToNonAD()
	}

	return sender.ToNonAD()
}

func content(msg *waE2E.Message) (string, *bot.Media) {
	switch {
	case msg.GetConversation() != "":
		return msg.GetConversation(), nil
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText(), nil
	case msg.GetImageMessage() != nil:
		img := msg.GetImageMessage()

		return img.GetCaption(), &bot.Media{Kind: bot.MediaImage, MIME: img.GetMimetype(), Raw: img}
	case msg.GetVideoMessage() != nil:
		vid := msg.GetVideoMessage()

		return vid.GetCaption(), &bot.Media{Kind: bot.MediaVideo, MIME: vid.GetMimetype(), Raw: vid}
	case msg.GetDocumentMessage() != nil:
		doc := msg.GetDocumentMessage()

		return doc.GetCaption(), &bot.Media{Kind: bot.MediaDocument, MIME: doc.GetMimetype(), Raw: doc}
	case msg.GetAudioMessage() != nil:
		aud := msg.GetAudioMessage()

		return "", &bot.Media{Kind: bot.MediaAudio, MIME: aud.GetMimetype(), Raw: aud}
	case msg.GetStickerMessage() != nil:
		sticker := msg.GetStickerMessage()

		return "", &bot.Media{Kind: bot.MediaSticker, MIME: sticker.GetMimetype(), Raw: sticker}
	default:
		return "", nil
	}
}
