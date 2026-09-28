package flow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
	"github.com/totallynotdavid/botkit/internal/flow/names"
)

const (
	// messageTimeout bounds one message: the turn, the download and the replies.
	messageTimeout = 30 * time.Second

	// fallbackLimit is how many messages in a row the flow may fail to
	// understand before the user is handed to a person.
	fallbackLimit = 3

	// staleAfter is how long a conversation may rest before the next message
	// starts it again from the start node.
	staleAfter = 24 * time.Hour

	// newcomerMessages is how many stored messages a user may have and still be
	// welcomed instead of welcomed back.
	newcomerMessages = 2
)

// What the bot says when the flow does not say it.
const (
	textUnsupportedMedia = "Lo siento, no puedo procesar ese tipo de mensaje. Por favor, envíame un mensaje de texto. 😊"
	textInvalidName      = "No pude reconocer eso como un nombre. ¿Podrías intentarlo de nuevo, por favor?"
	textActionFailed     = "Hubo un problema al procesar tu mensaje. Una asesora te ayudará a continuar. Disculpa las molestias."
	textWrongMedia       = "Parece que enviaste un tipo de archivo incorrecto. Por favor, asegúrate de enviar %s para que pueda procesarlo. Gracias 😊"
	textMenuFallback     = "No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\n"

	greetingNew  = "¡Bienvenidx! Es un placer ayudarte a empezar."
	greetingBack = "Qué gusto verte de nuevo."
	nameUnknown  = "amigx"
	courseUnset  = "el curso seleccionado"
)

// mediaNames says each kind of media the way the wrong-media reply asks for it.
//
//nolint:gochecknoglobals // a read-only table.
var mediaNames = map[bot.MediaKind]string{
	bot.MediaImage:    "una *imagen* (foto)",
	bot.MediaVideo:    "un *video*",
	bot.MediaAudio:    "un *audio*",
	bot.MediaDocument: "un *documento*",
	bot.MediaSticker:  "un *sticker*",
}

var _ bot.App = (*App)(nil)

// App walks users through a flow: each message is one Store turn that routes
// the message, runs the actions on the way, and stores the exchange. The
// replies are sent once the turn is stored.
type App struct {
	flow    *fsm.Flow
	store   *Store
	actions *Actions
	delay   time.Duration
	log     *slog.Logger
}

// New returns an App for flow. It fails when flow names an action that actions
// does not know, so a mistyped flow stops the bot at startup.
func New(flow *fsm.Flow, store *Store, actions *Actions, cfg Config, log *slog.Logger) (*App, error) {
	err := actions.Check(flow)
	if err != nil {
		return nil, fmt.Errorf("check flow: %w", err)
	}

	if log == nil {
		log = slog.Default()
	}

	return &App{flow: flow, store: store, actions: actions, delay: cfg.TypingDelay, log: log}, nil
}

// Handle answers one message. The turn is stored before any reply is sent, so
// a reply the user sees is one the history holds. A reply that cannot be sent
// is returned and the turn stays stored.
//
// An action that fails does not fail the turn: the user is escalated to a
// person and told so, and Handle returns the action's error once that is done.
// A name that does not look like one is not a failure, only a question asked
// again. If the turn cannot be stored, a voucher it saved is removed.
func (a *App) Handle(ctx context.Context, msg bot.Message, chat *bot.Chat) error {
	ctx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()

	run := &turn{app: a, chat: chat, msg: msg, log: a.log.With("user", msg.User)}

	err := a.store.Turn(ctx, msg.User, func(state *State) ([]StoredMessage, error) {
		run.play(ctx, state)

		return run.stored, nil
	})
	if err != nil {
		return errors.Join(err, run.discardVoucher())
	}

	run.log.DebugContext(ctx, "message handled", "node", run.state.CurrentNode, "replies", len(run.replies))

	return errors.Join(a.send(ctx, chat, run.replies), run.failure)
}

// send sends each reply after the typing delay. The delay ends early when ctx
// does, and then nothing more is sent.
func (a *App) send(ctx context.Context, chat *bot.Chat, replies []string) error {
	for _, text := range replies {
		err := a.typing(ctx)
		if err != nil {
			return err
		}

		err = chat.Send(ctx, text)
		if err != nil {
			return err //nolint:wrapcheck // Chat.Send wraps it.
		}
	}

	return nil
}

func (a *App) typing(ctx context.Context) error {
	if a.delay <= 0 {
		return nil
	}

	timer := time.NewTimer(a.delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait to reply: %w", context.Cause(ctx))
	}
}

// turn is what happens for one message inside Store.Turn.
type turn struct {
	app  *App
	chat *bot.Chat
	msg  bot.Message
	log  *slog.Logger

	state         *State
	voucherBefore string

	// stored and replies are the turn's messages, in order. A reply is stored
	// and sent, and a silent turn has none.
	stored  []StoredMessage
	replies []string
	// failure is what the actions of the turn returned, apart from a name that
	// was not a name.
	failure error
}

// play runs the message against state, which it leaves as the turn ends.
func (t *turn) play(ctx context.Context, state *State) {
	t.state = state
	t.voucherBefore = state.VoucherPath

	from := state.CurrentNode
	fresh := from == "" || time.Since(state.LastUpdated) > staleAfter

	var startErr error

	if fresh {
		from = t.app.flow.StartNode
		startErr = t.restart(ctx)
	}

	t.store(Inbound, t.msg.Text, from, t.msg.Time)

	if startErr != nil {
		t.fail(ctx, from, startErr)

		return
	}

	if fresh {
		t.finish(from, t.text(ctx, from))
	} else if t.rejectMedia(from) {
		return
	}

	route := t.app.flow.DetermineNext(from, t.msg.Text, t.mediaKind())
	if fresh && route.Via == fsm.ViaFallback {
		// The first message has answered nothing the greeting asked. It is stored
		// and does not count against the user.
		return
	}

	t.respond(ctx, from, route)
}

// restart puts the user on the start node and runs its action. A new user takes
// their profile name, and a returning one keeps the name they gave.
func (t *turn) restart(ctx context.Context) error {
	start := t.app.flow.StartNode
	isNew := t.state.CurrentNode == ""

	t.log.InfoContext(ctx, "starting the conversation", "node", start, "new", isNew)

	if isNew {
		t.state.UserName = t.msg.PushName
	}

	t.state.CurrentNode = start
	t.state.RepromptCount = 0

	return t.enter(ctx, start, "")
}

// respond answers a message that routed from node from along route. A message
// the flow does not understand counts toward escalation, and any other resets
// the count.
func (t *turn) respond(ctx context.Context, from string, route fsm.Route) {
	if route.Via != fsm.ViaFallback {
		t.state.RepromptCount = 0
		t.advance(ctx, from, route.Node, route.Action)

		return
	}

	t.state.RepromptCount++
	t.log.DebugContext(ctx, "fallback", "node", from, "action", route.Action, "count", t.state.RepromptCount)

	switch {
	case t.state.RepromptCount >= fallbackLimit:
		t.log.WarnContext(ctx, "user is stuck in a fallback loop, escalating", "node", from)

		t.state.RepromptCount = 0
		t.advance(ctx, from, fsm.HelpNode, actionEscalate)
	case route.Action == fsm.ActionFallbackWrongMedia:
		t.finish(from, fmt.Sprintf(textWrongMedia, t.expectedMedia(from)))
	default:
		t.fallback(ctx, from)
	}
}

// advance moves the user from node from to node target, running the action of
// the transition and then the action of the node entered. When an action fails
// the user is asked for the name again, or escalated to a person.
func (t *turn) advance(ctx context.Context, from, target, action string) {
	err := t.apply(ctx, action, from)
	if err == nil {
		err = t.enter(ctx, target, action)
	}

	if err != nil {
		t.fail(ctx, from, err)

		return
	}

	t.finish(target, t.text(ctx, target))
}

// fail answers an action that failed at node from. A name that was not a name is
// asked for again, and any other failure escalates the user to a person.
func (t *turn) fail(ctx context.Context, from string, err error) {
	if errors.Is(err, ErrInvalidName) {
		t.finish(from, textInvalidName)

		return
	}

	t.failure = errors.Join(t.failure, err, t.apply(ctx, actionEscalate, from))
	t.finish(fsm.HelpNode, textActionFailed)
}

// enter runs the action of node, unless skip already ran it as the action of
// the transition that led there.
func (t *turn) enter(ctx context.Context, node, skip string) error {
	action := t.app.flow.Nodes[node].Action
	if action == skip {
		return nil
	}

	return t.apply(ctx, action, node)
}

// apply runs action, which came from node, and logs it when it fails.
func (t *turn) apply(ctx context.Context, action, node string) error {
	err := t.app.actions.Apply(ctx, t.chat, action, t.state, t.msg, node)
	if err == nil {
		return nil
	}

	level := slog.LevelError
	if errors.Is(err, ErrInvalidName) {
		level = slog.LevelWarn
	}

	t.log.Log(ctx, level, "action failed", "node", node, "action", action, "error", err)

	return fmt.Errorf("action %q at node %q: %w", action, node, err)
}

// fallback answers a message the node did not understand. A node with nothing
// to offer stays silent, one with a message of its own for this says it, and
// otherwise the options of the node are asked again.
func (t *turn) fallback(ctx context.Context, from string) {
	node := t.app.flow.Nodes[from]

	switch {
	case len(node.Transitions) == 0 && node.IncludeTransitions == "":
		// The conversation has probably ended, as with a thanks. The user can
		// start again with a global keyword.
		t.log.DebugContext(ctx, "fallback in a node with no way out, staying silent", "node", from)
		t.finish(from, "")
	case node.FallbackMessage != "":
		t.finish(from, t.render(ctx, node.FallbackMessage))
	case node.Message.Content != "":
		t.finish(from, t.render(ctx, textMenuFallback+node.Message.Content))
	default:
		t.log.WarnContext(ctx, "fallback in a node with no message, sending the user to the start", "node", from)

		start := t.app.flow.StartNode
		t.finish(start, t.text(ctx, start))
	}
}

// rejectMedia asks for text when the message carries audio, a sticker, a video
// or a document and the node takes no media at all. A node that takes some
// leaves the message to the router, which accepts the kind it asked for and
// names it when it gets another. It reports whether it answered.
func (t *turn) rejectMedia(from string) bool {
	kind := t.mediaKind()
	if kind == "" || kind == bot.MediaImage {
		return false
	}

	takesMedia := slices.ContainsFunc(t.app.flow.Nodes[from].Transitions, func(tr fsm.Transition) bool {
		return tr.Condition.Type == fsm.ConditionMedia || tr.Condition.Type == fsm.ConditionMediaType
	})
	if takesMedia {
		return false
	}

	t.log.Info("unsupported media, asking for text", "node", from, "media", kind)
	t.finish(from, textUnsupportedMedia)

	return true
}

// expectedMedia lists the kinds of media node accepts, for the wrong-media reply.
func (t *turn) expectedMedia(node string) string {
	var asked []string

	for _, tr := range t.app.flow.Nodes[node].Transitions {
		if tr.Condition.Type != fsm.ConditionMediaType {
			continue
		}

		for _, kind := range tr.Condition.Value {
			name := mediaNames[bot.MediaKind(kind)]
			if !slices.Contains(asked, name) {
				asked = append(asked, name)
			}
		}
	}

	return strings.Join(asked, " o ")
}

func (t *turn) mediaKind() bot.MediaKind {
	if t.msg.Media == nil {
		return ""
	}

	return t.msg.Media.Kind
}

// store adds a message of the turn to the history.
func (t *turn) store(direction Direction, content, node string, at time.Time) {
	t.stored = append(t.stored, StoredMessage{Timestamp: at, Direction: direction, Content: content, NodeID: node})
}

// finish leaves the user in node and answers with text. An empty text is no
// answer: nothing is stored or sent.
func (t *turn) finish(node, text string) {
	t.state.CurrentNode = node

	if text == "" {
		return
	}

	t.store(Outbound, text, node, time.Now())
	t.replies = append(t.replies, text)
}

// text renders the message of node.
func (t *turn) text(ctx context.Context, node string) string {
	return t.render(ctx, t.app.flow.Nodes[node].Message.Content)
}

// render fills in the template values a flow's messages may use.
func (t *turn) render(ctx context.Context, content string) string {
	if content == "" {
		return ""
	}

	name := names.FirstName(t.state.UserName)
	if name == "" {
		name = nameUnknown
	}

	data := map[string]string{"name": name, "greeting": t.greeting(ctx)}

	if course := t.state.SelectedCourseID; course != "" {
		data["course_name"] = t.app.flow.Nodes[course].Title
		if data["course_name"] == "" {
			t.log.WarnContext(ctx, "selected course has no title", "course", course)

			data["course_name"] = courseUnset
		}
	}

	return render(content, data)
}

// greeting welcomes a user with little history and welcomes back one with more.
func (t *turn) greeting(ctx context.Context) string {
	count, err := t.app.store.MessageCount(ctx, t.state.UserID)
	if err != nil {
		t.log.ErrorContext(ctx, "cannot count messages for the greeting", "error", err)

		return greetingBack
	}

	if count <= newcomerMessages {
		return greetingNew
	}

	return greetingBack
}

// discardVoucher removes the voucher the turn saved, after the turn or its
// save failed and no state points to the file.
func (t *turn) discardVoucher() error {
	if t.state == nil || t.state.VoucherPath == t.voucherBefore {
		return nil
	}

	err := os.Remove(t.state.VoucherPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove voucher of an unsaved turn: %w", err)
	}

	return nil
}
