// Package fake is an in-memory bot.Client for tests. Tests deliver messages
// and session events by hand and read back what the bot sent; nothing reaches
// the network.
package fake

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// OwnJID is the account a paired fake client is logged in as.
const OwnJID bot.JID = "51900000000@s.whatsapp.net"

var (
	// ErrNotConnected is returned by sends outside Connect and Disconnect.
	ErrNotConnected = errors.New("fake: not connected")
	// ErrForeignMedia is returned by Download for media whose Raw is not the
	// []byte the fake expects.
	ErrForeignMedia = errors.New("fake: media Raw is not []byte")
)

var _ bot.Client = (*Client)(nil)

// Sent is one message the bot sent. Exactly one of Text and Image is set.
type Sent struct {
	To    bot.JID
	Text  string
	Image *bot.Image
}

// Reaction is one reaction the bot set.
type Reaction struct {
	Chat      bot.JID
	MessageID string
	Emoji     string
}

// Client records sends and reactions and delivers events to the handler
// passed to Connect. It is safe for concurrent use.
type Client struct {
	mu        sync.Mutex
	paired    bool
	stalled   bool
	onConnect []bot.Event
	handle    func(bot.Event)
	dialing   func(bot.Event) // the handler of a stalled Connect, until it returns
	dialUp    chan struct{}   // closed once a Connect is stalled
	up        chan struct{}   // closed while connected
	sendErr   error
	sent      []Sent
	reactions []Reaction
	lastID    int
}

// New returns a client paired as OwnJID.
func New() *Client {
	return &Client{paired: true, up: make(chan struct{}), dialUp: make(chan struct{})}
}

// NewUnpaired returns a client whose store holds no device, so Connect fails
// with NotPaired.
func NewUnpaired() *Client {
	return &Client{up: make(chan struct{}), dialUp: make(chan struct{})}
}

// StallConnect makes Connect block until its context is done and then return
// the context's error, like a dial that hangs. Events emitted meanwhile reach
// the handler, as they do from a real client that is still dialing.
func (c *Client) StallConnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stalled = true
}

// EmitOnConnect queues events that Connect emits after it has attached the
// handler and before it returns, like a connection that reports something
// during its handshake.
func (c *Client) EmitOnConnect(events ...bot.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.onConnect = append(c.onConnect, events...)
}

func (c *Client) Connect(ctx context.Context, handle func(bot.Event)) error {
	stalled, queued, err := c.attach(handle)
	if err != nil {
		return err
	}

	if stalled {
		<-ctx.Done()

		c.mu.Lock()
		c.dialing = nil
		c.mu.Unlock()

		return fmt.Errorf("connect: %w", ctx.Err())
	}

	for _, evt := range queued {
		handle(evt)
	}

	return nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.handle != nil {
		c.up = make(chan struct{})
	}

	c.handle = nil
}

// WaitConnected blocks until the client is connected or ctx is done. Tests
// call it after starting Run so events are not delivered before Connect.
func (c *Client) WaitConnected(ctx context.Context) error {
	c.mu.Lock()
	up := c.up
	c.mu.Unlock()

	select {
	case <-up:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for connection: %w", ctx.Err())
	}
}

// WaitDialing blocks until a stalled Connect is waiting or ctx is done.
func (c *Client) WaitDialing(ctx context.Context) error {
	select {
	case <-c.dialUp:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for dial: %w", ctx.Err())
	}
}

// Connected reports whether the client is between Connect and Disconnect.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.handle != nil
}

func (c *Client) OwnJID() bot.JID {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.paired {
		return ""
	}

	return OwnJID
}

// Emit hands evt to the connected handler and returns once the handler does.
// Like a closed connection, a disconnected client drops it.
func (c *Client) Emit(evt bot.Event) {
	c.mu.Lock()

	handle := c.handle
	if handle == nil {
		handle = c.dialing
	}

	c.mu.Unlock()

	if handle != nil {
		handle(evt)
	}
}

// Deliver emits msg as a received message and returns it as delivered. Empty
// fields are filled the way a direct message arrives: a generated ID, Chat
// and User equal to Sender, and the current time.
func (c *Client) Deliver(msg bot.Message) bot.Message {
	c.mu.Lock()
	if msg.ID == "" {
		c.lastID++
		msg.ID = "FAKE" + strconv.Itoa(c.lastID)
	}
	c.mu.Unlock()

	if msg.Chat == "" && !msg.Group {
		msg.Chat = msg.Sender
	}

	if msg.User == "" {
		msg.User = msg.Sender
	}

	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}

	c.Emit(bot.MessageReceived{Message: msg})

	return msg
}

// EndSession emits a SessionEnded event, as WhatsApp does when the device is
// unlinked, replaced or banned.
func (c *Client) EndSession(reason bot.SessionEndReason, detail string) {
	c.Emit(bot.SessionEnded{Reason: reason, Detail: detail})
}

// FailSends makes every later send and reaction return err, until it is
// called again with nil.
func (c *Client) FailSends(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sendErr = err
}

func (c *Client) SendText(_ context.Context, recipient bot.JID, text string) error {
	return c.record(func() { c.sent = append(c.sent, Sent{To: recipient, Text: text}) })
}

func (c *Client) SendImage(_ context.Context, recipient bot.JID, img bot.Image) error {
	return c.record(func() { c.sent = append(c.sent, Sent{To: recipient, Image: &img}) })
}

func (c *Client) React(_ context.Context, msg bot.Message, emoji string) error {
	return c.record(func() {
		c.reactions = append(c.reactions, Reaction{Chat: msg.Chat, MessageID: msg.ID, Emoji: emoji})
	})
}

// Download returns media.Raw, which tests set to the media's bytes.
func (c *Client) Download(_ context.Context, media *bot.Media) ([]byte, error) {
	data, ok := media.Raw.([]byte)
	if !ok {
		return nil, ErrForeignMedia
	}

	return data, nil
}

// Sent returns what the bot sent, in order.
func (c *Client) Sent() []Sent {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]Sent(nil), c.sent...)
}

// Reactions returns the reactions the bot set, in order.
func (c *Client) Reactions() []Reaction {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]Reaction(nil), c.reactions...)
}

func (c *Client) record(add func()) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.handle == nil {
		return ErrNotConnected
	}

	if c.sendErr != nil {
		return c.sendErr
	}

	add()

	return nil
}

// attach connects the handler unless the client is stalled, which it reports
// so Connect can wait outside the lock. It also returns the events Connect
// emits once the lock is released.
func (c *Client) attach(handle func(bot.Event)) (stalled bool, queued []bot.Event, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.paired {
		return false, nil, &bot.SessionEndedError{Reason: bot.NotPaired}
	}

	if c.handle != nil {
		return false, nil, bot.ErrAlreadyConnected
	}

	if c.stalled {
		c.dialing = handle

		select {
		case <-c.dialUp:
		default:
			close(c.dialUp)
		}

		return true, nil, nil
	}

	c.handle = handle
	close(c.up)

	return false, c.onConnect, nil
}
