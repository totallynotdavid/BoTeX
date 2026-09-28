package flow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/flow/names"
)

// voucherDirMode lets others enter the voucher directory. The vouchers in it
// are private to the bot's user: see writeVoucher.
const voucherDirMode os.FileMode = 0o755

// The errors actions wrap.
var (
	ErrUnknownAction = errors.New("unknown action")
	ErrInvalidName   = errors.New("invalid name")
)

// voucherUnsafe matches what a voucher's file name may not carry from a
// profile name.
var voucherUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// Downloader fetches the bytes of a message's media. *bot.Chat satisfies it.
type Downloader interface {
	Download(ctx context.Context, media *bot.Media) ([]byte, error)
}

// actionEscalate hands the user to a person. The app runs it when a user is
// stuck or an action fails, and the help node names it too.
const actionEscalate = "escalate_to_human_agent"

// handler carries out one action. msg is the message that took the user along
// the transition and origin is the node the action came from. media fetches
// the message's attachment.
type handler func(ctx context.Context, media Downloader, state *State, msg bot.Message, origin string) error

// Actions applies the actions of a flow to a user's State.
type Actions struct {
	voucherDir string
	// handlers is the whole vocabulary a flow file may use. Apply and Check both
	// read it, so what one runs is what the other accepts.
	handlers map[string]handler
}

// NewActions returns Actions that save vouchers in voucherDir, which is
// created when the first voucher arrives.
func NewActions(voucherDir string) *Actions {
	actions := &Actions{voucherDir: voucherDir}

	actions.handlers = map[string]handler{
		// A new lead is a fact for the log. The state holds nothing to record.
		"create_new_lead":               stateOnly(func(*State, string) {}),
		"clear_user_name":               stateOnly(func(state *State, _ string) { state.UserName = "" }),
		"set_selected_course":           stateOnly(func(state *State, origin string) { state.SelectedCourseID = origin }),
		"update_lead_interest_beginner": stateOnly(func(state *State, _ string) { state.CourseInterest = "beginner" }),
		"update_lead_interest_advanced": stateOnly(func(state *State, _ string) { state.CourseInterest = "advanced" }),
		"update_lead_consulted_price":   stateOnly(func(state *State, _ string) { state.ConsultedPrice = true }),
		actionEscalate:                  stateOnly(func(state *State, _ string) { state.RequiresHumanAgent = true }),
		"save_user_name": func(_ context.Context, _ Downloader, state *State, msg bot.Message, _ string) error {
			return saveUserName(state, msg.Text)
		},
		"save_payment_voucher": func(ctx context.Context, media Downloader, state *State, msg bot.Message, _ string) error {
			return actions.savePaymentVoucher(ctx, media, state, msg)
		},
	}

	return actions
}

// stateOnly adapts an action that changes the state and needs nothing else.
func stateOnly(change func(state *State, origin string)) handler {
	return func(_ context.Context, _ Downloader, state *State, _ bot.Message, origin string) error {
		change(state, origin)

		return nil
	}
}

// Check reports every action flow names that Apply does not know, each with
// where it sits, so a mistyped action fails when the flow loads and not when a
// user first reaches it.
func (a *Actions) Check(flow *fsm.Flow) error {
	var errs []error

	for _, use := range flow.Actions() {
		if a.handlers[use.Action] == nil {
			errs = append(errs, fmt.Errorf("%s: %w: %q", use.Where, ErrUnknownAction, use.Action))
		}
	}

	return errors.Join(errs...)
}

// Apply runs action on state. msg is the message that took the user along the
// transition, and origin is the node the action came from. media fetches the
// attachment of msg: it is per call because the connection that received msg
// answers it. An empty action does nothing. A message that cannot carry out
// the action is reported as an error and leaves state as it was.
func (a *Actions) Apply(ctx context.Context, media Downloader, action string, state *State, msg bot.Message, origin string) error {
	if action == "" {
		return nil
	}

	run := a.handlers[action]
	if run == nil {
		return fmt.Errorf("%w: %q", ErrUnknownAction, action)
	}

	return run(ctx, media, state, msg, origin)
}

// saveUserName stores what the user typed as their name, without an
// introduction like "me llamo". Text with no name in it keeps the old name,
// and text with no plausible name is an error.
func saveUserName(state *State, text string) error {
	name := introduction(strings.TrimSpace(text))
	if name == "" {
		return nil
	}

	if names.FirstName(name) == "" {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}

	state.UserName = name

	return nil
}

// introduction drops a leading "mi nombre es", "me llamo" or "llámame", ignoring
// case, when it is a word of its own.
func introduction(text string) string {
	for _, prefix := range [...]string{"mi nombre es", "me llamo", "llámame"} {
		if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
			continue
		}

		rest := text[len(prefix):]
		if next, _ := utf8.DecodeRuneInString(rest); rest == "" || unicode.IsSpace(next) {
			return strings.TrimSpace(rest)
		}
	}

	return text
}

// savePaymentVoucher stores the image of msg as the user's voucher. Anything
// else escalates to a person, who then asks for the voucher.
func (a *Actions) savePaymentVoucher(ctx context.Context, media Downloader, state *State, msg bot.Message) error {
	if msg.Media == nil || msg.Media.Kind != bot.MediaImage {
		state.RequiresHumanAgent = true

		return nil
	}

	data, err := media.Download(ctx, msg.Media)
	if err != nil {
		return fmt.Errorf("download voucher: %w", err)
	}

	err = os.MkdirAll(a.voucherDir, voucherDirMode)
	if err != nil {
		return fmt.Errorf("create voucher directory: %w", err)
	}

	path, err := writeVoucher(a.voucherDir, voucherPattern(state.UserID, msg.PushName), data)
	if err != nil {
		return fmt.Errorf("save voucher file: %w", err)
	}

	state.VoucherPath = path

	return nil
}

// writeVoucher stores data in a new file in dir named after pattern, as
// os.CreateTemp does. The file is created exclusively, so a voucher never
// replaces another one, and with mode 0600, because a voucher shows payment
// details.
func writeVoucher(dir, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}

	_, err = file.Write(data)

	err = errors.Join(err, file.Close())
	if err != nil {
		return "", fmt.Errorf("write: %w", errors.Join(err, os.Remove(file.Name())))
	}

	return file.Name(), nil
}

// voucherPattern names a voucher {phone}_{profile name}_{unix seconds}_*.jpeg,
// where * becomes a random number. The profile name keeps only ASCII letters,
// digits, hyphens and underscores, and spaces become underscores.
func voucherPattern(user bot.JID, pushName string) string {
	phone, _, _ := strings.Cut(string(user), "@")

	name := strings.Trim(voucherUnsafe.ReplaceAllString(strings.ReplaceAll(pushName, " ", "_"), ""), "_")
	if name == "" {
		name = "user"
	}

	return fmt.Sprintf("%s_%s_%d_*.jpeg", phone, name, time.Now().Unix())
}
