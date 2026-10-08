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

	// staleAfter is how long a conversation may rest before the next message
	// starts it again from the start node.
	staleAfter = 24 * time.Hour

	// newcomerMessages is how many stored messages a user may have and still be
	// welcomed instead of welcomed back.
	newcomerMessages = 2

	// followUpInterval is deliberately long. Proactive messages are a privilege
	// granted by the user, not a second conversation loop.
	followUpInterval = 24 * time.Hour
)

// What the bot says when the flow does not say it.
const (
	textUnsupportedMedia = "Lo siento, no puedo procesar ese tipo de mensaje. Por favor, envíame un mensaje de texto. 😊"
	textInvalidName      = "No pude reconocer eso como un nombre. ¿Podrías intentarlo de nuevo, por favor?"
	textActionRetry      = "No pude procesar eso todavía. Inténtalo una vez más o escribe *ayuda* y lo dejo preparado para una persona."
	textWrongMedia       = "Parece que enviaste un tipo de archivo incorrecto. Por favor, asegúrate de enviar %s para que pueda procesarlo. Gracias 😊"
	textMenuFallback     = "No entendí tu respuesta 😊 Por favor, revisa las opciones:\n\n"

	greetingNew  = "¡Bienvenidx! Es un placer ayudarte a empezar."
	greetingBack = "Qué gusto verte de nuevo."
	nameUnknown  = "amigx"
	courseUnset  = "el curso seleccionado"
	personaName  = "Luma"
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
// An action that fails keeps the user in the guided step and asks for a retry;
// Handle returns the action's error after storing that recovery reply. A name
// that does not look like one is not a failure, only a question asked again.
// If the turn cannot be stored, a voucher it saved is removed.
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

	sendErr := a.send(ctx, chat, run.replies)
	if sendErr == nil && run.success {
		sendErr = chat.React(ctx, "✅")
	}

	return errors.Join(sendErr, run.failure)
}

// FollowUp sends one opt-in reminder per eligible user. The store claims each
// row before the send, so two schedulers cannot send the same reminder inside
// the rate window. The production CLI calls this from its hourly scheduler;
// ordinary inbound turns never trigger it implicitly.
func (a *App) FollowUp(ctx context.Context, transport bot.Transport, now time.Time) (int, error) {
	now = now.UTC()

	users, err := a.store.ClaimFollowUps(ctx, now, followUpInterval)
	if err != nil {
		return 0, err
	}

	sent := 0

	var sendErrs []error

	for _, user := range users {
		choice := a.choiceLabel(user.LastChoice)
		if choice == "" {
			choice = "algo nuevo"
		}

		text := fmt.Sprintf("Hola, soy %s 👋 ¿Seguimos con %s? Responde cuando te venga bien.", personaName, choice)

		err = transport.SendText(ctx, user.User, text)
		if err != nil {
			releaseErr := a.releaseFollowUp(ctx, user)
			sendErrs = append(sendErrs, errors.Join(fmt.Errorf("send follow-up to %s: %w", user.User, err), releaseErr))

			continue
		}

		sent++

		err = a.store.RecordFollowUp(ctx, user.User, now, text)
		if err != nil {
			// The claim is deliberately retained: the transport already accepted
			// the reminder, so retrying would violate at-most-once delivery.
			sendErrs = append(sendErrs, fmt.Errorf("store follow-up to %s: %w", user.User, err))

			continue
		}
	}

	return sent, errors.Join(sendErrs...)
}

func (a *App) releaseFollowUp(ctx context.Context, user FollowUp) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), messageTimeout)
	defer cancel()

	return a.store.ReleaseFollowUp(releaseCtx, user.User, user.ClaimedAt)
}

// send sends each reply after the typing delay. The delay ends early when ctx
// does, and then nothing more is sent.
func (a *App) send(ctx context.Context, chat *bot.Chat, replies []string) error {
	var errs []error

	for _, text := range replies {
		err := a.typing(ctx)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		err = chat.Send(ctx, text)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
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
	// success controls the post-save reaction. Fallbacks and failed actions
	// never set it.
	success bool
}

// play runs the message against state, which it leaves as the turn ends.
func (t *turn) play(ctx context.Context, state *State) {
	t.state = state
	t.voucherBefore = state.VoucherPath

	from := state.CurrentNode

	lastActivity := state.LastUpdated
	if state.LastFollowUp.After(lastActivity) {
		lastActivity = state.LastFollowUp
	}

	fresh := from == "" || time.Now().UTC().Sub(lastActivity.UTC()) > staleAfter

	var startErr error

	if fresh {
		from = t.app.flow.StartNode
		startErr = t.restart(ctx)
	}

	t.store(Inbound, t.msg.Text, from, t.msg.Time)

	if startErr != nil {
		t.fail(from, startErr)

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

	return t.enter(ctx, start, "")
}

// respond answers a message that routed from node from along route. An
// unrecognised message is repaired in place.
func (t *turn) respond(ctx context.Context, from string, route fsm.Route) {
	if route.Via != fsm.ViaFallback {
		t.advance(ctx, from, route.Node, route.Action)

		return
	}

	t.log.DebugContext(ctx, "fallback", "node", from, "action", route.Action)

	if route.Action == fsm.ActionFallbackWrongMedia {
		t.finish(from, fmt.Sprintf(textWrongMedia, t.expectedMedia(from)))

		return
	}

	t.fallback(ctx, from)
}

// advance moves the user from node from to node target, running the action of
// the transition and then the action of the node entered. When an action fails
// the user stays in the guided step and gets a retry prompt.
func (t *turn) advance(ctx context.Context, from, target, action string) {
	err := t.apply(ctx, action, from)
	if err == nil {
		err = t.enter(ctx, target, action)
	}

	if err != nil {
		t.fail(from, err)

		return
	}

	t.finish(target, t.text(ctx, target))
	t.success = actionIsSuccessful(action) || actionIsSuccessful(t.app.flow.Nodes[target].Action)
}

func actionIsSuccessful(action string) bool {
	switch action {
	case "create_new_lead", "save_user_name", "set_selected_course",
		"update_lead_interest_beginner", "update_lead_interest_advanced", "save_payment_voucher":
		return true
	default:
		return false
	}
}

// fail answers an action that failed at node from. A name that was not a name
// is asked for again, and any other failure gets a retry prompt.
func (t *turn) fail(from string, err error) {
	if errors.Is(err, ErrInvalidName) {
		t.finish(from, textInvalidName)

		return
	}

	// A transient download or action failure is recoverable. Keep the user in
	// the guided step and make the next valid attempt possible.
	t.failure = errors.Join(t.failure, err)
	t.finish(from, textActionRetry)
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
	err := t.app.actions.apply(ctx, t.chat, action, t.state, t.msg, node)
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

	historyHint, err := t.app.store.historyHint(ctx, t.state.UserID)
	if err != nil {
		t.log.WarnContext(ctx, "cannot load history hint", "error", err)
	}

	lastChoice := t.app.choiceLabel(t.state.LastChoice)
	if lastChoice == "" {
		lastChoice = "ninguna opción todavía"
	}

	data := map[string]string{
		"name":         name,
		"greeting":     t.greeting(ctx),
		"persona":      personaName,
		"last_choice":  lastChoice,
		"history_hint": historyHint,
	}

	if course := t.state.SelectedCourseID; course != "" {
		data["course_name"] = t.app.flow.Nodes[course].Title
		if data["course_name"] == "" {
			t.log.WarnContext(ctx, "selected course has no title", "course", course)

			data["course_name"] = courseUnset
		}
	}

	return render(content, data)
}

func (a *App) choiceLabel(choice string) string {
	if choice == "" {
		return ""
	}

	if node, ok := a.flow.Nodes[choice]; ok && node.Title != "" {
		return node.Title
	}

	return choice
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
