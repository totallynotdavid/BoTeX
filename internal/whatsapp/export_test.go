package whatsapp

import (
	"context"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/totallynotdavid/botkit/internal/bot"
)

//nolint:ireturn // bot.Event is the sealed set translate maps into.
func Translate(evt any) (bot.Event, bool) { return translate(evt) }

//nolint:ireturn // the tests drive the logger through whatsmeow's interface.
func NewLogger(log *slog.Logger, module string) waLog.Logger {
	return waLogger{log: log, module: module}
}

// PairOffline makes c's device look paired as own and calls dial in place of
// opening whatsmeow's socket.
func (c *Client) PairOffline(own types.JID, dial func(context.Context) error) {
	c.wa.Store.ID = &own
	c.dial = dial
}

// Dispatch hands evt to the handlers whatsmeow has registered, as its socket
// would.
func (c *Client) Dispatch(evt any) {
	//nolint:staticcheck // the only way to reach whatsmeow's handlers without a socket.
	c.wa.DangerousInternals().DispatchEvent(evt)
}

func (c *Client) PutLIDMapping(ctx context.Context, lid, phone types.JID) error {
	err := c.lids.PutLIDMapping(ctx, lid, phone)
	if err != nil {
		return fmt.Errorf("put LID mapping: %w", err)
	}

	return nil
}

func (c *Client) IsConnected() bool { return c.wa.IsConnected() }
