package whatsapp_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

const (
	alicePhone = "51900000001@s.whatsapp.net"
	aliceLID   = "200000000000001@lid"
	groupChat  = "120363000000000001@g.us"
)

func jid(raw string) types.JID {
	parsed, err := types.ParseJID(raw)
	if err != nil {
		panic(err)
	}

	return parsed
}

func sentAt() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }

func incoming(chat, sender types.JID, msg *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: chat.Server == types.GroupServer},
			ID:            "MSG1",
			PushName:      "Alice",
			Timestamp:     sentAt(),
		},
		Message: msg,
	}
}

func text(body string) *waE2E.Message { return &waE2E.Message{Conversation: new(body)} }

// direct is a text message Alice sent to the bot, edited by change.
func direct(change func(*bot.Message)) bot.Message {
	msg := bot.Message{
		ID: "MSG1", Chat: alicePhone, Sender: alicePhone, User: alicePhone, PushName: "Alice", Time: sentAt(),
	}
	change(&msg)

	return msg
}

func TestTranslateMessages(t *testing.T) {
	t.Parallel()

	alice := jid(alicePhone)
	lid := jid(aliceLID)

	aliceDevice := alice
	aliceDevice.Device = 12

	fromLIDWithPhone := incoming(lid, lid, text("hi"))
	fromLIDWithPhone.Info.SenderAlt = alice

	fromMe := incoming(alice, alice, text("!help"))
	fromMe.Info.IsFromMe = true

	image := &waE2E.ImageMessage{Caption: new("look"), Mimetype: new("image/jpeg")}
	audio := &waE2E.AudioMessage{Mimetype: new("audio/ogg")}
	doc := &waE2E.DocumentMessage{Caption: new("voucher"), Mimetype: new("application/pdf")}

	tests := []struct {
		name string
		evt  *events.Message
		want bot.Message
	}{
		{
			"conversation",
			incoming(alice, alice, text("!latex x^2")),
			direct(func(msg *bot.Message) { msg.Text = "!latex x^2" }),
		},
		{
			"extended text",
			incoming(alice, alice, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: new("quoted reply")}}),
			direct(func(msg *bot.Message) { msg.Text = "quoted reply" }),
		},
		{
			"image with caption",
			incoming(alice, alice, &waE2E.Message{ImageMessage: image}),
			direct(func(msg *bot.Message) {
				msg.Text = "look"
				msg.Media = &bot.Media{Kind: bot.MediaImage, MIME: "image/jpeg", Raw: image}
			}),
		},
		{
			"audio without text",
			incoming(alice, alice, &waE2E.Message{AudioMessage: audio}),
			direct(func(msg *bot.Message) { msg.Media = &bot.Media{Kind: bot.MediaAudio, MIME: "audio/ogg", Raw: audio} }),
		},
		{
			"document with caption",
			incoming(alice, alice, &waE2E.Message{DocumentMessage: doc}),
			direct(func(msg *bot.Message) {
				msg.Text = "voucher"
				msg.Media = &bot.Media{Kind: bot.MediaDocument, MIME: "application/pdf", Raw: doc}
			}),
		},
		{
			"group message from a device",
			incoming(jid(groupChat), aliceDevice, text("hola")),
			direct(func(msg *bot.Message) {
				msg.Chat = groupChat
				msg.Group = true
				msg.Text = "hola"
			}),
		},
		{
			"LID sender with phone number",
			fromLIDWithPhone,
			direct(func(msg *bot.Message) {
				msg.Chat = aliceLID
				msg.Sender = aliceLID
				msg.Text = "hi"
			}),
		},
		{
			"LID sender without phone number",
			incoming(lid, lid, text("hi")),
			direct(func(msg *bot.Message) {
				msg.Chat = aliceLID
				msg.Sender = aliceLID
				msg.User = aliceLID
				msg.Text = "hi"
			}),
		},
		{
			"own message",
			fromMe,
			direct(func(msg *bot.Message) {
				msg.FromMe = true
				msg.Text = "!help"
			}),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := whatsapp.Translate(testCase.evt)
			if !ok {
				t.Fatal("message dropped")
			}

			want := bot.MessageReceived{Message: testCase.want}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Translate() =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

func TestTranslateDropsMessagesWithoutContent(t *testing.T) {
	t.Parallel()

	alice := jid(alicePhone)
	newsletter := types.NewJID("120363000000000002", types.NewsletterServer)

	tests := []struct {
		name string
		evt  *events.Message
	}{
		{"status update", incoming(types.StatusBroadcastJID, alice, text("my status"))},
		{"newsletter post", incoming(newsletter, newsletter, text("news"))},
		{"reaction", incoming(alice, alice, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: new("👍")}})},
		{"protocol message", incoming(alice, alice, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}})},
		{"blank text", incoming(alice, alice, text("  \n"))},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := whatsapp.Translate(testCase.evt)
			if ok {
				t.Errorf("Translate() = %+v, want dropped", got)
			}
		})
	}
}

func TestTranslateConnectionEvents(t *testing.T) {
	t.Parallel()

	for evt, want := range map[any]bot.Event{
		&events.Connected{}:    bot.Connected{},
		&events.Disconnected{}: bot.Disconnected{},
	} {
		got, ok := whatsapp.Translate(evt)
		if !ok || got != want {
			t.Errorf("Translate(%T) = %#v, %t, want %#v", evt, got, ok, want)
		}
	}
}

func TestTranslateSessionEnds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		evt       any
		reason    bot.SessionEndReason
		detailHas string
	}{
		{
			name:      "logged out on connect",
			evt:       &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut},
			reason:    bot.LoggedOut,
			detailHas: "401",
		},
		{
			name:      "device removed while connected",
			evt:       &events.LoggedOut{OnConnect: false, Reason: events.ConnectFailureLoggedOut},
			reason:    bot.LoggedOut,
			detailHas: "401",
		},
		{
			name:      "main device gone",
			evt:       &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureMainDeviceGone},
			reason:    bot.LoggedOut,
			detailHas: "403",
		},
		{name: "replaced", evt: &events.StreamReplaced{}, reason: bot.Replaced},
		{
			name:      "temporary ban",
			evt:       &events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: 2 * time.Hour},
			reason:    bot.Banned,
			detailHas: "2h0m0s",
		},
		{name: "outdated", evt: &events.ClientOutdated{}, reason: bot.Outdated},
		{
			name:      "unknown connect failure",
			evt:       &events.ConnectFailure{Reason: events.ConnectFailureReason(499), Message: "nope"},
			reason:    bot.Refused,
			detailHas: "499",
		},
		{
			name:      "token refresh failed",
			evt:       &events.CATRefreshError{Error: context.DeadlineExceeded},
			reason:    bot.Refused,
			detailHas: "CAT refresh failed",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, _ := whatsapp.Translate(testCase.evt)

			ended, ok := got.(bot.SessionEnded)
			if !ok {
				t.Fatalf("Translate() = %#v, want a SessionEnded", got)
			}

			if ended.Reason != testCase.reason {
				t.Errorf("reason = %s, want %s", ended.Reason, testCase.reason)
			}

			if !strings.Contains(ended.Detail, testCase.detailHas) {
				t.Errorf("detail = %q, want it to contain %q", ended.Detail, testCase.detailHas)
			}
		})
	}
}

func TestTranslateIgnoresOtherEvents(t *testing.T) {
	t.Parallel()

	for _, evt := range []any{
		&events.Receipt{},
		&events.KeepAliveTimeout{ErrorCount: 1},
		&events.PushName{},
		&events.QR{Codes: []string{"never shown"}},
	} {
		got, ok := whatsapp.Translate(evt)
		if ok {
			t.Errorf("Translate(%T) = %#v, want ignored", evt, got)
		}
	}
}
