package bot

import (
	"context"
	"fmt"
)

// App handles messages the runtime accepted. Apps are built in cmd with their
// dependencies, so there is no Init or Env grab-bag.
//
// ctx is cancelled when the runtime stops. After a session ends,
// context.Cause(ctx) is the *SessionEndedError.
type App interface {
	Handle(ctx context.Context, m Message, c *Chat) error
}

// Chat is what an App uses to answer the message it is handling. The runtime
// builds one per message around the Transport it was given. Once the session has
// ended, every method fails with the session's *SessionEndedError.
type Chat struct {
	client Transport
	msg    Message
	// ended returns the session's end, or nil while it lasts.
	ended func() *SessionEndedError
}

// Send replies with text in the chat the message came from.
func (c *Chat) Send(ctx context.Context, text string) error {
	err := c.check()
	if err != nil {
		return err
	}

	err = c.client.SendText(ctx, c.msg.Chat, text)
	if err != nil {
		return fmt.Errorf("send text: %w", err)
	}

	return nil
}

// SendImage replies with img in the chat the message came from.
func (c *Chat) SendImage(ctx context.Context, img Image) error {
	err := c.check()
	if err != nil {
		return err
	}

	err = c.client.SendImage(ctx, c.msg.Chat, img)
	if err != nil {
		return fmt.Errorf("send image: %w", err)
	}

	return nil
}

// React sets emoji as the bot's reaction to the message being handled. An
// empty emoji removes it.
func (c *Chat) React(ctx context.Context, emoji string) error {
	err := c.check()
	if err != nil {
		return err
	}

	err = c.client.React(ctx, c.msg, emoji)
	if err != nil {
		return fmt.Errorf("react: %w", err)
	}

	return nil
}

// Download fetches the bytes of an attachment of a received message.
func (c *Chat) Download(ctx context.Context, media *Media) ([]byte, error) {
	err := c.check()
	if err != nil {
		return nil, err
	}

	data, err := c.client.Download(ctx, media)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	return data, nil
}

// check returns an error rather than a *SessionEndedError so a nil result is
// a nil error, not a non-nil interface.
func (c *Chat) check() error {
	end := c.ended()
	if end != nil {
		return end
	}

	return nil
}
