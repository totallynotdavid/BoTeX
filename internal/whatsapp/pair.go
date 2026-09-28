package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"golang.org/x/term"
)

const (
	// pairDisplayName is what the phone's linked-devices list shows for a
	// pairing code. WhatsApp validates it as "Browser (OS)".
	pairDisplayName = "Chrome (Linux)"
)

var (
	// ErrNotTerminal is returned by Pair when stdin or stdout is not a
	// terminal, so nobody could scan the code.
	ErrNotTerminal = errors.New("pairing needs an interactive terminal on stdin and stdout")

	// ErrAlreadyPaired is returned by Pair when the store already holds a
	// device. Pairing again would replace it.
	ErrAlreadyPaired = errors.New("the store already holds a paired device")

	// ErrInvalidPhone is returned by Pair for a phone number that is not "+"
	// followed by digits.
	ErrInvalidPhone = errors.New("phone must be an international number: + followed by digits")

	// ErrPairTimeout is returned by Pair when the codes expire unused.
	ErrPairTimeout = errors.New("pairing timed out")

	// ErrPairFailed is returned by Pair when WhatsApp ends the pairing.
	ErrPairFailed = errors.New("pairing failed")
)

var phonePattern = regexp.MustCompile(`^\+\d{7,15}$`)

// PairOptions says how Pair talks to the operator.
type PairOptions struct {
	// Phone is the account's number, "+" and digits. Pair shows a pairing code
	// to type on that phone. Empty shows a QR code to scan instead.
	Phone string
	// In and Out are the operator's terminal. Pair refuses anything else.
	In, Out *os.File
}

// Pair links a new device to a WhatsApp account and stores its keys in
// database. It returns once the phone confirms, or with an error.
//
// It refuses before opening the store or any connection when In or Out is not
// a terminal, or Phone is malformed, and before connecting when the store
// already holds a device.
func Pair(ctx context.Context, database *sql.DB, log *slog.Logger, opts PairOptions) error {
	if opts.Phone != "" && !phonePattern.MatchString(opts.Phone) {
		return fmt.Errorf("%w: %q", ErrInvalidPhone, opts.Phone)
	}

	if !term.IsTerminal(int(opts.In.Fd())) || !term.IsTerminal(int(opts.Out.Fd())) {
		return ErrNotTerminal
	}

	client, err := Open(ctx, database, log)
	if err != nil {
		return err
	}

	if client.wa.Store.ID != nil {
		return fmt.Errorf("%w: %s", ErrAlreadyPaired, client.OwnJID())
	}

	return client.pair(ctx, opts)
}

func (c *Client) pair(ctx context.Context, opts PairOptions) error {
	// The channel must exist before Connect: it listens for the events the
	// dial produces.
	items, err := c.wa.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("listen for pairing events: %w", err)
	}

	err = c.dial(ctx)
	if err != nil {
		return fmt.Errorf("connect to whatsapp: %w", err)
	}

	defer c.wa.Disconnect()

	codeShown := false

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("pair: %w", context.Cause(ctx))
		case item, open := <-items:
			if !open {
				return ErrPairFailed
			}

			done, err := c.onPairItem(ctx, item, opts, &codeShown)
			if done || err != nil {
				return err
			}
		}
	}
}

// onPairItem reports whether the pairing is over, and how it failed if it did.
func (c *Client) onPairItem(ctx context.Context, item whatsmeow.QRChannelItem, opts PairOptions, codeShown *bool) (bool, error) {
	switch item.Event {
	case whatsmeow.QRChannelEventCode:
		if opts.Phone == "" {
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, opts.Out)

			return false, printLine(opts.Out, "Scan the code in WhatsApp: Settings, Linked devices, Link a device.")
		}

		// One pairing code covers the whole window, so later QR items add
		// nothing.
		if *codeShown {
			return false, nil
		}

		*codeShown = true

		return false, c.showPairingCode(ctx, opts)
	case whatsmeow.QRChannelSuccess.Event:
		return true, printLine(opts.Out, "Paired as "+string(c.OwnJID())+".")
	case whatsmeow.QRChannelTimeout.Event:
		return true, ErrPairTimeout
	case whatsmeow.QRChannelEventError:
		return true, fmt.Errorf("%w: %w", ErrPairFailed, item.Error)
	default:
		return true, fmt.Errorf("%w: %s", ErrPairFailed, item.Event)
	}
}

func (c *Client) showPairingCode(ctx context.Context, opts PairOptions) error {
	code, err := c.wa.PairPhone(ctx, opts.Phone, true, whatsmeow.PairClientChrome, pairDisplayName)
	if err != nil {
		return fmt.Errorf("request pairing code: %w", err)
	}

	return printLine(opts.Out, "Enter this code in WhatsApp: Settings, Linked devices, Link with phone number: "+code)
}

func printLine(out *os.File, text string) error {
	_, err := fmt.Fprintln(out, text)
	if err != nil {
		return fmt.Errorf("write to terminal: %w", err)
	}

	return nil
}
