package fake_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/whatsapp/fake"
)

const alice bot.JID = "51900000001@s.whatsapp.net"

// errRejected stands in for a send WhatsApp refused.
var errRejected = errors.New("rejected")

func TestUnpairedConnectFails(t *testing.T) {
	t.Parallel()

	client := fake.NewUnpaired()

	err := client.Connect(t.Context(), func(bot.Event) {})

	var ended *bot.SessionEndedError
	if !errors.As(err, &ended) || ended.Reason != bot.NotPaired {
		t.Fatalf("Connect() error = %v, want reason not_paired", err)
	}

	if client.Connected() || client.OwnJID() != "" {
		t.Errorf("unpaired client: Connected() = %v, OwnJID() = %q", client.Connected(), client.OwnJID())
	}
}

func TestEventsReachHandlerOnlyWhileConnected(t *testing.T) {
	t.Parallel()

	client := fake.New()

	var got []bot.Event

	client.Deliver(bot.Message{Sender: alice, Text: "before connect"})

	err := client.Connect(t.Context(), func(evt bot.Event) { got = append(got, evt) })
	if err != nil {
		t.Fatal(err)
	}

	msg := client.Deliver(bot.Message{Sender: alice, Text: "!help"})
	client.EndSession(bot.LoggedOut, "401")
	client.Disconnect()
	client.Deliver(bot.Message{Sender: alice, Text: "after disconnect"})

	want := []bot.Event{
		bot.MessageReceived{Message: msg},
		bot.SessionEnded{Reason: bot.LoggedOut, Detail: "401"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("handler got %#v\nwant %#v", got, want)
	}

	if msg.ID == "" || msg.Chat != alice || msg.User != alice || msg.Time.IsZero() {
		t.Errorf("Deliver did not fill a direct message's defaults: %+v", msg)
	}
}

func TestSecondConnectIsRefusedUntilDisconnect(t *testing.T) {
	t.Parallel()

	client := fake.New()

	var first, second int

	err := client.Connect(t.Context(), func(bot.Event) { first++ })
	if err != nil {
		t.Fatal(err)
	}

	err = client.Connect(t.Context(), func(bot.Event) { second++ })
	if !errors.Is(err, bot.ErrAlreadyConnected) {
		t.Fatalf("second Connect() error = %v, want ErrAlreadyConnected", err)
	}

	client.Deliver(bot.Message{Sender: alice, Text: "one"})
	client.Disconnect()

	err = client.Connect(t.Context(), func(bot.Event) { second++ })
	if err != nil {
		t.Fatalf("Connect() after Disconnect: %v", err)
	}

	client.Deliver(bot.Message{Sender: alice, Text: "two"})

	if first != 1 || second != 1 {
		t.Errorf("first handler got %d events, second got %d; want 1 each", first, second)
	}
}

func TestDeliverKeepsGroupChat(t *testing.T) {
	t.Parallel()

	client := fake.New()
	connect(t, client)

	msg := client.Deliver(bot.Message{Chat: "120363000000000001@g.us", Sender: alice, Group: true, Text: "hi"})
	if msg.Chat != "120363000000000001@g.us" {
		t.Errorf("Chat = %q, want the group", msg.Chat)
	}
}

func TestSendsAreRecorded(t *testing.T) {
	t.Parallel()

	client := fake.New()
	connect(t, client)

	msg := client.Deliver(bot.Message{Sender: alice, Text: "!latex x"})
	img := bot.Image{Data: []byte("png"), MIME: "image/png"}

	for _, err := range []error{
		client.React(t.Context(), msg, "✅"),
		client.SendText(t.Context(), alice, "done"),
		client.SendImage(t.Context(), alice, img),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	wantSent := []fake.Sent{{To: alice, Text: "done"}, {To: alice, Image: &img}}
	if got := client.Sent(); !reflect.DeepEqual(got, wantSent) {
		t.Errorf("Sent() = %+v, want %+v", got, wantSent)
	}

	wantReactions := []fake.Reaction{{Chat: alice, MessageID: msg.ID, Emoji: "✅"}}
	if got := client.Reactions(); !reflect.DeepEqual(got, wantReactions) {
		t.Errorf("Reactions() = %+v, want %+v", got, wantReactions)
	}
}

func TestSendFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*testing.T, *fake.Client)
		want  error
	}{
		{
			name:  "before connect",
			setup: func(*testing.T, *fake.Client) {},
			want:  fake.ErrNotConnected,
		},
		{
			name: "after disconnect",
			setup: func(t *testing.T, client *fake.Client) {
				t.Helper()
				connect(t, client)
				client.Disconnect()
			},
			want: fake.ErrNotConnected,
		},
		{
			name: "failing sends",
			setup: func(t *testing.T, client *fake.Client) {
				t.Helper()
				connect(t, client)
				client.FailSends(errRejected)
			},
			want: errRejected,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := fake.New()
			testCase.setup(t, client)

			for _, err := range []error{
				client.SendText(t.Context(), alice, "x"),
				client.SendImage(t.Context(), alice, bot.Image{}),
				client.React(t.Context(), bot.Message{Chat: alice, ID: "1"}, "❌"),
			} {
				if !errors.Is(err, testCase.want) {
					t.Errorf("error = %v, want %v", err, testCase.want)
				}
			}

			if len(client.Sent()) != 0 || len(client.Reactions()) != 0 {
				t.Error("a failed send was recorded")
			}
		})
	}
}

func TestFailSendsNilRestoresSends(t *testing.T) {
	t.Parallel()

	client := fake.New()
	connect(t, client)
	client.FailSends(errRejected)
	client.FailSends(nil)

	err := client.SendText(t.Context(), alice, "back")
	if err != nil {
		t.Fatalf("SendText() = %v after FailSends(nil)", err)
	}
}

func TestDownload(t *testing.T) {
	t.Parallel()

	client := fake.New()

	got, err := client.Download(t.Context(), &bot.Media{Kind: bot.MediaImage, Raw: []byte("jpeg")})
	if err != nil || string(got) != "jpeg" {
		t.Errorf("Download() = %q, %v; want the Raw bytes", got, err)
	}

	_, err = client.Download(t.Context(), &bot.Media{Kind: bot.MediaImage, Raw: "not bytes"})
	if !errors.Is(err, fake.ErrForeignMedia) {
		t.Errorf("Download() error = %v, want ErrForeignMedia", err)
	}
}

func TestHandlerMaySendConcurrently(t *testing.T) {
	t.Parallel()

	const deliveries = 50

	client := fake.New()

	var (
		sends sync.WaitGroup
		errs  = make(chan error, deliveries)
	)

	err := client.Connect(t.Context(), func(evt bot.Event) {
		received, ok := evt.(bot.MessageReceived)
		if !ok {
			return
		}

		sends.Go(func() { errs <- client.SendText(t.Context(), received.Message.Chat, received.Message.Text) })
	})
	if err != nil {
		t.Fatal(err)
	}

	for range deliveries {
		client.Deliver(bot.Message{Sender: alice, Text: "x"})
	}

	sends.Wait()
	close(errs)

	for sendErr := range errs {
		if sendErr != nil {
			t.Errorf("SendText() = %v", sendErr)
		}
	}

	if count := len(client.Sent()); count != deliveries {
		t.Errorf("recorded %d sends, want %d", count, deliveries)
	}
}

func connect(t *testing.T, client *fake.Client) {
	t.Helper()

	err := client.Connect(t.Context(), func(bot.Event) {})
	if err != nil {
		t.Fatal(err)
	}
}
