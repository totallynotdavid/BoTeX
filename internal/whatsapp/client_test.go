package whatsapp_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/sqlite"
	"github.com/totallynotdavid/botkit/internal/whatsapp"
)

// These tests never reach WhatsApp. An unpaired store makes Connect return
// before whatsmeow dials, and a store paired offline never opens a socket.
//
// Opening is serialised: creating a device signs a key through libsignal,
// whose package-level logger is initialised without a lock. A process opens
// one store, so only parallel tests can race on it.
//
//nolint:gochecknoglobals // guards a package-level race inside libsignal.
var openMu sync.Mutex

const ownPhone = "51900000000@s.whatsapp.net"

func openUnpaired(t *testing.T, path string) *whatsapp.Client {
	t.Helper()

	database, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	openMu.Lock()
	client, err := whatsapp.Open(t.Context(), database, slog.New(slog.DiscardHandler))
	openMu.Unlock()

	if err != nil {
		t.Fatal(err)
	}

	return client
}

// pairedOffline returns a client on the store at path that Connect accepts
// without a socket, and the number of times Connect dialled.
func pairedOffline(t *testing.T, path string) (*whatsapp.Client, *atomic.Int32) {
	t.Helper()

	client := openUnpaired(t, path)

	var dials atomic.Int32

	client.PairOffline(jid(ownPhone), func(context.Context) error {
		dials.Add(1)

		return nil
	})
	t.Cleanup(client.Disconnect)

	return client, &dials
}

func countMessages(count *atomic.Int32) func(bot.Event) {
	return func(evt bot.Event) {
		if _, ok := evt.(bot.MessageReceived); ok {
			count.Add(1)
		}
	}
}

func TestConnectUnpaired(t *testing.T) {
	t.Parallel()

	client := openUnpaired(t, filepath.Join(t.TempDir(), "bot.db"))

	err := client.Connect(t.Context(), func(evt bot.Event) { t.Errorf("unexpected event %#v", evt) })

	var ended *bot.SessionEndedError
	if !errors.As(err, &ended) || ended.Reason != bot.NotPaired {
		t.Fatalf("Connect() error = %v, want a SessionEndedError with reason not_paired", err)
	}

	if client.IsConnected() {
		t.Error("client connected without a paired device")
	}

	if got := client.OwnJID(); got != "" {
		t.Errorf("OwnJID() = %q, want empty before pairing", got)
	}
}

func TestOpenReusesExistingStore(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bot.db")
	openUnpaired(t, path)
	openUnpaired(t, path)
}

func TestSecondConnectDeliversEachEventOnce(t *testing.T) {
	t.Parallel()

	client, dials := pairedOffline(t, filepath.Join(t.TempDir(), "bot.db"))
	msg := incoming(jid(alicePhone), jid(alicePhone), text("hi"))

	var first, second atomic.Int32

	err := client.Connect(t.Context(), countMessages(&first))
	if err != nil {
		t.Fatal(err)
	}

	err = client.Connect(t.Context(), countMessages(&second))
	if !errors.Is(err, bot.ErrAlreadyConnected) {
		t.Fatalf("second Connect() error = %v, want ErrAlreadyConnected", err)
	}

	if dials.Load() != 1 {
		t.Errorf("dialled %d times, want 1: a refused Connect must not dial", dials.Load())
	}

	client.Dispatch(msg)
	client.Disconnect()
	client.Dispatch(msg)

	err = client.Connect(t.Context(), countMessages(&second))
	if err != nil {
		t.Fatalf("Connect() after Disconnect: %v", err)
	}

	client.Dispatch(msg)

	if first.Load() != 1 || second.Load() != 1 {
		t.Errorf("first handler got %d messages, second got %d; want 1 each", first.Load(), second.Load())
	}
}

func TestSessionOutlivesConnectContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bot.db")
	lid := jid(aliceLID)

	err := openUnpaired(t, path).PutLIDMapping(t.Context(), lid, jid(alicePhone))
	if err != nil {
		t.Fatal(err)
	}

	// A second client on the store starts with an empty LID cache, so its
	// lookup below reads the database under the session's context.
	client, _ := pairedOffline(t, path)

	received := make(chan bot.Message, 1)
	connectCtx, cancel := context.WithCancel(t.Context())

	err = client.Connect(connectCtx, func(evt bot.Event) {
		if got, ok := evt.(bot.MessageReceived); ok {
			received <- got.Message
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	cancel()
	client.Dispatch(incoming(lid, lid, text("hi")))

	msg := <-received
	if msg.User != alicePhone {
		t.Errorf("User = %q, want the phone-number JID looked up after Connect's ctx ended", msg.User)
	}

	if msg.Sender != aliceLID {
		t.Errorf("Sender = %q, want the LID the message came from", msg.Sender)
	}
}

func TestDisconnectEndsTheSessionContext(t *testing.T) {
	t.Parallel()

	client := openUnpaired(t, filepath.Join(t.TempDir(), "bot.db"))

	sessions := make(chan context.Context, 1)

	client.PairOffline(jid(ownPhone), func(ctx context.Context) error {
		sessions <- ctx

		return nil
	})

	err := client.Connect(t.Context(), func(bot.Event) {})
	if err != nil {
		t.Fatal(err)
	}

	session := <-sessions
	if session.Err() != nil {
		t.Fatalf("session context ended while connected: %v", session.Err())
	}

	client.Disconnect()

	if !errors.Is(session.Err(), context.Canceled) {
		t.Errorf("session context error after Disconnect = %v, want context.Canceled", session.Err())
	}
}

func TestConnectFailsWhenContextEnds(t *testing.T) {
	t.Parallel()

	client, _ := pairedOffline(t, filepath.Join(t.TempDir(), "bot.db"))
	msg := incoming(jid(alicePhone), jid(alicePhone), text("hi"))

	var got atomic.Int32

	ended, cancel := context.WithCancel(t.Context())
	cancel()

	err := client.Connect(ended, countMessages(&got))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect() error = %v, want context.Canceled", err)
	}

	client.Dispatch(msg)

	err = client.Connect(t.Context(), countMessages(&got))
	if err != nil {
		t.Fatalf("Connect() after a failed Connect: %v", err)
	}

	client.Dispatch(msg)

	if got.Load() != 1 {
		t.Errorf("got %d messages, want 1: the failed Connect left its handler registered", got.Load())
	}
}

func TestDownloadRejectsForeignMedia(t *testing.T) {
	t.Parallel()

	client := openUnpaired(t, filepath.Join(t.TempDir(), "bot.db"))

	_, err := client.Download(t.Context(), &bot.Media{Kind: bot.MediaImage, Raw: []byte("from the fake")})
	if !errors.Is(err, whatsapp.ErrForeignMedia) {
		t.Errorf("Download() error = %v, want ErrForeignMedia", err)
	}
}
