// Package whatsapp implements bot.Client over whatsmeow. It is the only
// package that imports whatsmeow.
package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// ErrForeignMedia is returned by Download for media another Client produced.
var ErrForeignMedia = errors.New("media did not come from this client")

var _ bot.Client = (*Client)(nil)

// Client is a WhatsApp session whose keys live in the bot's SQLite database.
type Client struct {
	wa *whatsmeow.Client
	// dial opens wa's socket. Tests replace it so nothing leaves the process.
	dial func(ctx context.Context) error
	// lids is the store's LID-to-phone map. A device only references it once
	// paired, so the client keeps its own handle.
	lids store.LIDStore
	log  *slog.Logger

	mu      sync.Mutex
	session *session
}

// session is one Connect's event handler and the context whatsmeow's loops
// and the handler's store lookups run under.
type session struct {
	handlerID uint32
	cancel    context.CancelFunc
}

// Open loads the session stored in database, creating whatsmeow's tables on
// first use. It does not connect. log receives whatsmeow's own logs.
func Open(ctx context.Context, database *sql.DB, log *slog.Logger) (*Client, error) {
	// "sqlite3" names whatsmeow's SQL dialect, not the database/sql driver.
	container := sqlstore.NewWithDB(database, "sqlite3", waLogger{log: log, module: "Database"})

	err := container.Upgrade(ctx)
	if err != nil {
		return nil, fmt.Errorf("upgrade whatsapp store: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("load whatsapp device: %w", err)
	}

	wa := whatsmeow.NewClient(device, waLogger{log: log, module: "Client"})

	return &Client{
		wa:   wa,
		dial: wa.ConnectContext,
		lids: container.LIDMap,
		log:  log,
	}, nil
}

func (c *Client) Connect(ctx context.Context, handle func(bot.Event)) error {
	if c.wa.Store.ID == nil {
		return &bot.SessionEndedError{Reason: bot.NotPaired}
	}

	// Held through the dial so a Disconnect cannot interleave with it.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session != nil {
		return bot.ErrAlreadyConnected
	}

	sessionCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.session = &session{handlerID: c.wa.AddEventHandler(c.receiver(sessionCtx, handle)), cancel: cancel}

	stopDial := context.AfterFunc(ctx, cancel)
	err := c.dial(sessionCtx)

	if !stopDial() && err == nil {
		// ctx ended as the dial finished and has already cancelled the session.
		err = ctx.Err()
	}

	if err != nil {
		c.endLocked()

		return fmt.Errorf("connect to whatsapp: %w", err)
	}

	return nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.endLocked()
}

func (c *Client) OwnJID() bot.JID {
	own := c.wa.Store.GetJID()
	if own.IsEmpty() {
		return ""
	}

	return bot.JID(own.ToNonAD().String())
}

func (c *Client) SendText(ctx context.Context, recipient bot.JID, text string) error {
	jid, err := parseJID(recipient)
	if err != nil {
		return err
	}

	_, err = c.wa.SendMessage(ctx, jid, &waE2E.Message{Conversation: new(text)})
	if err != nil {
		return fmt.Errorf("send text to %s: %w", recipient, err)
	}

	return nil
}

func (c *Client) SendImage(ctx context.Context, recipient bot.JID, img bot.Image) error {
	jid, err := parseJID(recipient)
	if err != nil {
		return err
	}

	uploaded, err := c.wa.Upload(ctx, img.Data, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("upload image for %s: %w", recipient, err)
	}

	msg := &waE2E.ImageMessage{
		Mimetype:      new(img.MIME),
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    new(uint64(len(img.Data))),
	}
	if img.Caption != "" {
		msg.Caption = new(img.Caption)
	}

	_, err = c.wa.SendMessage(ctx, jid, &waE2E.Message{ImageMessage: msg})
	if err != nil {
		return fmt.Errorf("send image to %s: %w", recipient, err)
	}

	return nil
}

func (c *Client) React(ctx context.Context, msg bot.Message, emoji string) error {
	chat, err := parseJID(msg.Chat)
	if err != nil {
		return err
	}

	sender, err := parseJID(msg.Sender)
	if err != nil {
		return err
	}

	_, err = c.wa.SendMessage(ctx, chat, c.wa.BuildReaction(chat, sender, msg.ID, emoji))
	if err != nil {
		return fmt.Errorf("react to %s in %s: %w", msg.ID, msg.Chat, err)
	}

	return nil
}

func (c *Client) Download(ctx context.Context, media *bot.Media) ([]byte, error) {
	downloadable, ok := media.Raw.(whatsmeow.DownloadableMessage)
	if !ok {
		return nil, ErrForeignMedia
	}

	data, err := c.wa.Download(ctx, downloadable)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", media.Kind, err)
	}

	return data, nil
}

// endLocked closes the socket and ends the running session, if any. c.mu must
// be held.
func (c *Client) endLocked() {
	if c.session == nil {
		return
	}

	c.wa.Disconnect()
	c.wa.RemoveEventHandler(c.session.handlerID)
	c.session.cancel()
	c.session = nil
}

func (c *Client) receiver(ctx context.Context, handle func(bot.Event)) func(any) {
	return func(raw any) {
		evt, ok := translate(raw)
		if !ok {
			return
		}

		if received, isMessage := evt.(bot.MessageReceived); isMessage {
			received.Message.User = c.phoneUser(ctx, received.Message.User)
			evt = received
		}

		handle(evt)
	}
}

// phoneUser maps a LID user to the phone number whatsmeow learned for it, for
// senders whose message did not carry the phone number itself.
func (c *Client) phoneUser(ctx context.Context, user bot.JID) bot.JID {
	lid, err := types.ParseJID(string(user))
	if err != nil || lid.Server != types.HiddenUserServer {
		return user
	}

	phone, err := c.lids.GetPNForLID(ctx, lid)
	if err != nil {
		c.log.WarnContext(ctx, "look up phone number for LID", "lid", user, "error", err)

		return user
	}

	if phone.IsEmpty() {
		return user
	}

	return bot.JID(phone.ToNonAD().String())
}

func parseJID(raw bot.JID) (types.JID, error) {
	jid, err := types.ParseJID(string(raw))
	if err != nil {
		return types.JID{}, fmt.Errorf("parse JID %q: %w", raw, err)
	}

	return jid, nil
}
